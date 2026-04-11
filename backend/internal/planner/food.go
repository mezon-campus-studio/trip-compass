package planner

import (
	"fmt"
	"strings"
	"tripcompass-backend/internal/models"
)

// DayFoodMap maps meal_type → []SlotPlace for one day.
type DayFoodMap map[string][]SlotPlace // key: "breakfast"|"lunch"|"dinner"

// BuildFoodMap assigns food venues to each day ensuring no global repeats.
// Returns map[dayNum]DayFoodMap and any violations (e.g. NO_FOOD_VENUES).
func BuildFoodMap(foodPlaces []models.Place, dayAssignments map[int][]SlotPlace, numDays int, dest string) (map[int]DayFoodMap, []Violation) {
	slots := toSlotPlaces(foodPlaces)
	var violations []Violation

	if len(slots) == 0 {
		violations = append(violations, Violation{
			Rule:     "NO_FOOD_VENUES",
			Severity: "warning",
			Message:  fmt.Sprintf("Không có địa điểm ăn uống nào trong DB cho '%s'", dest),
		})
		return map[int]DayFoodMap{}, violations
	}

	usedGlobal := map[string]bool{}
	result := map[int]DayFoodMap{}

	mealTypes := []string{"breakfast", "lunch", "dinner"}

	for day := 1; day <= numDays; day++ {
		dayMeals := DayFoodMap{}
		usedToday := map[string]bool{}

		attrSlots := dayAssignments[day]
		var centerLat, centerLng float64
		if len(attrSlots) > 0 && attrSlots[0].Lat != 0 {
			centerLat = attrSlots[0].Lat
			centerLng = attrSlots[0].Lng
		}

		for _, meal := range mealTypes {
			chosen := pickFood(slots, meal, centerLat, centerLng, usedGlobal, usedToday)
			if chosen != nil {
				usedGlobal[chosen.ID] = true
				usedToday[chosen.ID] = true
				dayMeals[meal] = []SlotPlace{*chosen}
			}
		}

		result[day] = dayMeals
	}

	return result, violations
}

// pickFood selects the best food venue for a meal type near a location.
func pickFood(
	venues []SlotPlace,
	mealType string,
	centerLat, centerLng float64,
	usedGlobal, usedToday map[string]bool,
) *SlotPlace {
	type scored struct {
		p     SlotPlace
		score float64
	}
	var candidates []scored

	for _, v := range venues {
		if usedGlobal[v.ID] || usedToday[v.ID] {
			continue
		}
		score := 0.0
		// Distance bonus: closer is better (inverse km)
		if centerLat != 0 && v.Lat != 0 {
			km := HaversineKm(centerLat, centerLng, v.Lat, v.Lng)
			if km < 0.1 {
				score += 10
			} else {
				score += 1.0 / km
			}
		}
		// Meal type keyword bonus
		score += mealTypeScore(v, mealType)
		candidates = append(candidates, scored{v, score})
	}

	if len(candidates) == 0 {
		// Fallback: pick any not used globally or today
		for _, v := range venues {
			if !usedGlobal[v.ID] && !usedToday[v.ID] {
				cp := v
				return &cp
			}
		}
		return nil
	}

	// Sort by score desc
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	cp := best.p
	return &cp
}

func mealTypeScore(v SlotPlace, mealType string) float64 {
	// Tags take priority over name keywords
	for _, tag := range v.Tags {
		switch mealType {
		case "breakfast":
			if tag == "breakfast-spot" || tag == "morning" || tag == "early-open" {
				return 3.0
			}
		case "lunch":
			if tag == "lunch-spot" || tag == "quick-meal" {
				return 3.0
			}
		case "dinner":
			if tag == "seafood" || tag == "dinner-spot" || tag == "bbq" || tag == "hotpot" {
				return 3.0
			}
		}
	}
	// Fallback: keyword in place name
	name := v.Name
	switch mealType {
	case "breakfast":
		for _, kw := range []string{"sáng", "bún", "bánh mì", "phở", "breakfast", "cháo"} {
			if strings.Contains(name, kw) {
				return 2.0
			}
		}
	case "dinner":
		for _, kw := range []string{"tối", "hải sản", "nướng", "lẩu", "dinner", "seafood"} {
			if strings.Contains(name, kw) {
				return 2.0
			}
		}
	}
	return 0
}

func toSlotPlaces(places []models.Place) []SlotPlace {
	result := make([]SlotPlace, 0, len(places))
	for _, p := range places {
		result = append(result, toSlotPlace(p))
	}
	return result
}
