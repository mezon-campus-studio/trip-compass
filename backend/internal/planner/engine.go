package planner

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"tripcompass-backend/internal/models"

	"gorm.io/gorm"
)

// Engine is the main planner orchestrator.
type Engine struct {
	db *gorm.DB
}

// NewEngine creates a new planner engine.
func NewEngine(db *gorm.DB) *Engine {
	return &Engine{db: db}
}

// Generate runs the full slot-assignment pipeline and returns a structured itinerary.
func (e *Engine) Generate(req GenerateRequest) (*GenerateResponse, error) {
	if req.GuestCount <= 0 {
		req.GuestCount = 2
	}

	numDays, err := computeNumDays(req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("invalid dates: %w", err)
	}
	if numDays < 1 {
		numDays = 1
	}

	dest := strings.ToLower(strings.TrimSpace(req.Destination))

	// 1. Query DB
	var attractions []models.Place
	e.db.Where("destination = ? AND category = ?", dest, models.CategoryAttraction).
		Order("must_visit DESC, priority_score DESC, rating DESC NULLS LAST").
		Find(&attractions)

	var foodPlaces []models.Place
	e.db.Where("destination = ? AND category = ?", dest, models.CategoryFood).
		Order("priority_score DESC, rating DESC NULLS LAST").
		Find(&foodPlaces)

	var combos []models.Combo
	e.db.Where("destination = ?", dest).
		Order("price_per_person ASC NULLS LAST").
		Find(&combos)

	// 2. Select attractions within budget (preference-aware scoring)
	selected := SelectAttractions(attractions, req.BudgetVND, req.GuestCount, req.PreferenceTags)

	// 3. Evaluate combos
	comboResult := EvaluateCombos(combos, selected, req.BudgetVND, req.GuestCount)

	// 4. Separate must_visit from regular
	mustVisit := []SlotPlace{}
	regular := []SlotPlace{}
	for _, p := range selected {
		if p.IsMustVisit {
			mustVisit = append(mustVisit, p)
		} else {
			regular = append(regular, p)
		}
	}

	// 5. Cluster regular by proximity (cluster-aware assignment preserves geo groups)
	clusters := ClusterByProximity(regular, 5.0)
	dayAssignments, dayDurationMap := assignToDays(mustVisit, clusters, numDays)

	// 6. Sort within each day: greedy NN seed → 2-opt improvement
	for day, places := range dayAssignments {
		if len(places) > 1 {
			ordered := SortByNearest(places, places[0].Lat, places[0].Lng)
			dayAssignments[day] = TwoOptImprove(ordered)
		}
	}

	// 6b. Inter-day swap optimization (reduces cross-day travel distance)
	SwapOptimize(dayAssignments, dayDurationMap, 3)

	// 7. Build food map
	foodMap, foodViolations := BuildFoodMap(foodPlaces, dayAssignments, numDays, dest)

	// 8. Estimate food spend per destination
	mealCost := MealCostVND(dest)
	foodSpent := mealCost * req.GuestCount * 3 * numDays

	// 8b. Fill empty standard days so no day is left completely empty
	fillEmptyDays(dayAssignments, selected, numDays)

	// 8c. Recalculate attrSpent from actual schedule (exclude dropped places)
	attrSpent := recalcAttrSpent(dayAssignments, req.GuestCount)

	// 9. Build day plans
	days := make([]DayPlan, 0, numDays)
	for d := 1; d <= numDays; d++ {
		attrSlots := dayAssignments[d]
		var foodSlots []SlotPlace
		if dm, ok := foodMap[d]; ok {
			for _, meal := range []string{"breakfast", "lunch", "dinner"} {
				if venues, ok := dm[meal]; ok {
					foodSlots = append(foodSlots, venues...)
				}
			}
		}
		dp := BuildDayPlan(d, numDays, attrSlots, foodSlots, req.StartDate, comboResult.IncludesLunch, req.ArrivalTime, req.DepartureTime)
		days = append(days, dp)
	}

	// 10. Validate
	violations := Validate(days, req.BudgetVND, attrSpent, foodSpent, dayAssignments)
	violations = append(violations, foodViolations...)

	// 11. Budget recap (dynamic ratio based on budget level + destination)
	attrRatio, _ := dynamicBudgetRatio(req.BudgetVND, dest, numDays, req.GuestCount)
	attrBudget := int(float64(req.BudgetVND) * attrRatio)
	foodBudget := FoodBudgetVND(req.BudgetVND)
	miscBudget := req.BudgetVND - attrBudget - foodBudget
	remaining := req.BudgetVND - attrSpent - foodSpent

	resp := &GenerateResponse{
		Days: days,
		BudgetRecap: BudgetRecap{
			TotalBudgetVND:      req.BudgetVND,
			AttractionBudgetVND: attrBudget,
			AttractionSpentVND:  attrSpent,
			FoodBudgetVND:       foodBudget,
			FoodSpentVND:        foodSpent,
			MiscBudgetVND:       miscBudget,
			RemainingVND:        remaining,
			WithinBudget:        (attrSpent + foodSpent) <= req.BudgetVND,
		},
		Violations: violations,
	}

	if comboResult != nil {
		resp.ComboResult = comboResult
	}

	return resp, nil
}

// assignToDays distributes must_visit and clusters across numDays.
// Returns both the day→places map and a dayDuration map for use in SwapOptimize.
//
// Strategy:
//  1. Full-day items (duration ≥ 360min) → dedicated middle days
//  2. Normal must_visit → capacity-sorted middle days
//  3. Clusters → cluster-aware: entire cluster to one day (not round-robin per item)
//  4. 2-day trip edge case: distribute evenly between arrival + departure
func assignToDays(mustVisit []SlotPlace, clusters [][]SlotPlace, numDays int) (map[int][]SlotPlace, map[int]int) {
	assignments := map[int][]SlotPlace{}
	dayDuration := map[int]int{}
	for d := 1; d <= numDays; d++ {
		assignments[d] = []SlotPlace{}
		dayDuration[d] = 0
	}

	// Middle days: skip day 1 (arrival) and day N (departure)
	middleDays := []int{}
	for d := 2; d < numDays; d++ {
		middleDays = append(middleDays, d)
	}
	// Edge: if only 1-2 days total, use all days
	allDays := []int{}
	for d := 1; d <= numDays; d++ {
		allDays = append(allDays, d)
	}
	if len(middleDays) == 0 {
		middleDays = allDays
	}

	addToDay := func(day int, places []SlotPlace) bool {
		total := 0
		for _, p := range places {
			total += activityDuration(p)
		}
		if dayDuration[day]+total <= maxDurationPerDay {
			assignments[day] = append(assignments[day], places...)
			dayDuration[day] += total
			return true
		}
		return false
	}

	// ── Step 1: collect ALL full-day items ──────────────────────────────────────
	fullDayItems := []SlotPlace{}
	normalClusters := [][]SlotPlace{}
	mustNormal := []SlotPlace{}

	for _, p := range mustVisit {
		if p.IsFullDay {
			fullDayItems = append(fullDayItems, p)
		} else {
			mustNormal = append(mustNormal, p)
		}
	}
	for _, cluster := range clusters {
		hasFullDay := false
		for _, p := range cluster {
			if p.IsFullDay {
				hasFullDay = true
				break
			}
		}
		if hasFullDay {
			fullDayItems = append(fullDayItems, cluster...)
		} else {
			normalClusters = append(normalClusters, cluster)
		}
	}
	// Sort full-day items by duration DESC
	sort.Slice(fullDayItems, func(i, j int) bool {
		return activityDuration(fullDayItems[i]) > activityDuration(fullDayItems[j])
	})

	// ── Step 2: full-day items → dedicated middle days ───────────────────────────
	for _, p := range fullDayItems {
		for _, day := range sortedByCapacity(middleDays, dayDuration) {
			if addToDay(day, []SlotPlace{p}) {
				break
			}
		}
	}

	// ── Step 3: normal must_visit → remaining middle-day capacity ────────────────
	for _, p := range mustNormal {
		assigned := false
		for _, day := range sortedByCapacity(middleDays, dayDuration) {
			if addToDay(day, []SlotPlace{p}) {
				assigned = true
				break
			}
		}
		if !assigned {
			addToDay(1, []SlotPlace{p}) // last resort: arrival day
		}
	}

	// ── Step 4: cluster-aware assignment (whole cluster per day) ─────────────────
	// Sort clusters by total duration DESC so largest clusters get first pick of days.
	sort.Slice(normalClusters, func(i, j int) bool {
		return clusterDuration(normalClusters[i]) > clusterDuration(normalClusters[j])
	})
	for _, cluster := range normalClusters {
		assigned := false
		// Try to fit entire cluster into one day (preserves geo locality)
		for _, day := range sortedByCapacity(allDays, dayDuration) {
			if addToDay(day, cluster) {
				assigned = true
				break
			}
		}
		if !assigned {
			// Fallback: assign items individually to best-capacity days
			for _, p := range cluster {
				for _, day := range sortedByCapacity(allDays, dayDuration) {
					if addToDay(day, []SlotPlace{p}) {
						break
					}
				}
			}
		}
	}

	// ── Step 5: 2-day trip edge case ─────────────────────────────────────────────
	// numDays==2 means only arrival + departure; no middle days.
	// Distribute must_visit evenly between day 1 and day 2.
	if numDays == 2 {
		for i, p := range mustNormal {
			if i%2 == 0 {
				addToDay(1, []SlotPlace{p})
			} else {
				addToDay(2, []SlotPlace{p})
			}
		}
	}

	return assignments, dayDuration
}

func computeNumDays(startDate, endDate string) (int, error) {
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return 0, err
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return 0, err
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 1 {
		return 1, nil
	}
	return days, nil
}

// fillEmptyDays ensures no standard day is left with zero activities.
// Uses a usedIDs set to prevent scheduling the same place on multiple days.
func fillEmptyDays(assignments map[int][]SlotPlace, selected []SlotPlace, numDays int) {
	if len(selected) == 0 {
		return
	}
	// Build set of already-scheduled place IDs
	usedIDs := map[string]bool{}
	for _, places := range assignments {
		for _, p := range places {
			usedIDs[p.ID] = true
		}
	}
	for d := 2; d < numDays; d++ { // skip day 1 (arrival) and last day (departure)
		if len(assignments[d]) > 0 {
			continue // already has activities
		}
		// Find next unused place that fits
		for _, p := range selected {
			if !usedIDs[p.ID] && activityDuration(p) <= maxDurationPerDay {
				assignments[d] = []SlotPlace{p}
				usedIDs[p.ID] = true
				break
			}
		}
	}
}

// recalcAttrSpent computes the actual attraction spend from scheduled places only.
// This excludes places that were selected but dropped from the schedule.
func recalcAttrSpent(assignments map[int][]SlotPlace, guestCount int) int {
	seen := map[string]bool{}
	spent := 0
	for _, places := range assignments {
		for _, p := range places {
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			spent += p.BasePrice * guestCount
		}
	}
	return spent
}

// sortedByCapacity returns a copy of days sorted by remaining capacity (most free first).
func sortedByCapacity(days []int, dayDuration map[int]int) []int {
	sorted := make([]int, len(days))
	copy(sorted, days)
	sort.Slice(sorted, func(i, j int) bool {
		return (maxDurationPerDay - dayDuration[sorted[i]]) > (maxDurationPerDay - dayDuration[sorted[j]])
	})
	return sorted
}

// clusterDuration returns the total activity duration of a cluster of places.
func clusterDuration(cluster []SlotPlace) int {
	total := 0
	for _, p := range cluster {
		total += activityDuration(p)
	}
	return total
}
