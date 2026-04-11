package planner

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultMorningStart = 8 * 60  // 08:00 in minutes
	defaultArrivalStart = 15 * 60 // 15:00 in minutes
	defaultLastDayStart = 7 * 60  // 07:00 in minutes
	lunchStartMin       = 11*60 + 30
	lunchEndMin         = 13 * 60
	dinnerStartMin      = 18 * 60
	dinnerEndMin        = 19*60 + 30
	eveningEndMin       = 21 * 60
	bufferBetweenMin    = 30  // buffer between activity and next event
	defaultActivityDur  = 120 // fallback if place has no duration
)

// minsToTime converts minutes-since-midnight to "HH:MM".
func minsToTime(m int) string {
	if m < 0 {
		m = 0
	}
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

// activityDuration returns a place's duration in minutes, with a sensible fallback.
func activityDuration(p SlotPlace) int {
	if p.Duration > 0 {
		return p.Duration
	}
	return defaultActivityDur
}

// BuildDayPlan constructs a DayPlan for one day with dynamic slot timing.
func BuildDayPlan(
	dayNum, totalDays int,
	places []SlotPlace,
	foodPlaces []SlotPlace,
	startDate string,
	comboIncludesLunch bool,
	arrivalTime string,
	departureTime string,
) DayPlan {
	dayType := determineDayType(dayNum, totalDays, places)
	date := computeDate(startDate, dayNum-1)

	var slots []TimeSlot

	switch dayType {
	case "arrival":
		slots = buildArrivalDay(places, foodPlaces, comboIncludesLunch, arrivalTime)
	case "departure":
		slots = buildDepartureDay(places, foodPlaces)
	case "full_day":
		slots = buildFullDay(places, foodPlaces, comboIncludesLunch)
	default:
		slots = buildStandardDay(places, foodPlaces, comboIncludesLunch)
	}

	travelMin := computeTravelMin(slots)
	bufferMin := computeBufferMin(slots)

	primaryArea := "center"
	if len(places) > 0 && places[0].Area != "" {
		primaryArea = places[0].Area
	}

	return DayPlan{
		DayNum:      dayNum,
		DateStr:     date,
		DayType:     dayType,
		Slots:       slots,
		PrimaryArea: primaryArea,
		TravelMin:   travelMin,
		BufferMin:   bufferMin,
	}
}

// buildStandardDay builds a full standard day schedule dynamically.
// Pattern: breakfast? → morning activity → lunch → afternoon activity → buffer → dinner → evening
func buildStandardDay(places, food []SlotPlace, comboIncludesLunch bool) []TimeSlot {
	slots := []TimeSlot{}
	cur := defaultMorningStart // start at 08:00
	foodIdx := 0
	placeIdx := 0

	// Morning activity
	if placeIdx < len(places) {
		p := places[placeIdx]
		placeIdx++
		dur := activityDuration(p)
		end := cur + dur
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      minsToTime(end),
			SlotType: "morning_activity",
			Place:    &p,
		})
		cur = end + bufferBetweenMin
	}

	// Lunch around 11:30–13:00; push if still in activity
	lunchStart := lunchStartMin
	if cur > lunchStart {
		lunchStart = cur
	}
	lunchEnd := lunchStart + 90
	if comboIncludesLunch {
		slots = append(slots, TimeSlot{
			Start:        minsToTime(lunchStart),
			End:          minsToTime(lunchEnd),
			SlotType:     "lunch",
			ComboCovered: true,
		})
	} else if foodIdx < len(food) {
		f := food[foodIdx]
		foodIdx++
		slots = append(slots, TimeSlot{
			Start:    minsToTime(lunchStart),
			End:      minsToTime(lunchEnd),
			SlotType: "lunch",
			Place:    &f,
		})
	}
	cur = lunchEnd + 30 // 30-min lunch rest

	// Afternoon activity — only if we still have time before dinner buffer (17:30)
	afternoonCutoff := 17*60 + 30
	if placeIdx < len(places) && cur < afternoonCutoff {
		// P3.2: Add realistic travel buffer between morning and afternoon activity
		if placeIdx > 0 {
			prev := places[placeIdx-1]
			next := places[placeIdx]
			if prev.Lat != 0 && next.Lat != 0 {
				travelMin := EstimateTravelMin(HaversineKm(prev.Lat, prev.Lng, next.Lat, next.Lng))
				if travelMin > bufferBetweenMin {
					// Add explicit travel slot
					slots = append(slots, TimeSlot{
						Start:    minsToTime(cur),
						End:      minsToTime(cur + travelMin),
						SlotType: "travel",
						IsBuffer: true,
					})
					cur += travelMin
				}
			}
		}
		p := places[placeIdx]
		placeIdx++
		dur := activityDuration(p)
		end := cur + dur
		// Cap at 17:30 to leave room for dinner
		if end > afternoonCutoff {
			end = afternoonCutoff
		}
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      minsToTime(end),
			SlotType: "afternoon_activity",
			Place:    &p,
		})
		cur = end
	}

	// Buffer before dinner — only add if we have space
	if cur < dinnerStartMin {
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      minsToTime(dinnerStartMin),
			SlotType: "buffer",
			IsBuffer: true,
		})
	}

	// Dinner — start at 18:00 or after current time if we ran late
	dinnerStart := dinnerStartMin
	if cur > dinnerStartMin {
		dinnerStart = cur
	}
	actualDinnerEnd := dinnerStart + 90
	if foodIdx < len(food) {
		f := food[foodIdx]
		foodIdx++
		slots = append(slots, TimeSlot{
			Start:    minsToTime(dinnerStart),
			End:      minsToTime(actualDinnerEnd),
			SlotType: "dinner",
			Place:    &f,
		})
	}

	// Evening — starts after dinner actually ends; add short activity if available
	eveningActivityEndMax := 21 * 60 // cap at 21:00
	if placeIdx < len(places) {
		p := places[placeIdx]
		dur := activityDuration(p)
		// Only short places (≤60min) suitable for evening and still open
		closingOK := p.Hours == "" || closingHourMin(p.Hours) >= eveningActivityEndMax
		if dur <= 60 && closingOK && actualDinnerEnd+dur <= eveningActivityEndMax {
			placeIdx++
			slots = append(slots, TimeSlot{
				Start:    minsToTime(actualDinnerEnd),
				End:      minsToTime(actualDinnerEnd + dur),
				SlotType: "evening_activity",
				Place:    &p,
			})
			actualDinnerEnd += dur
		}
	}
	// Evening free block (if any time left before 21:00)
	if actualDinnerEnd < eveningEndMin {
		slots = append(slots, TimeSlot{
			Start:    minsToTime(actualDinnerEnd),
			End:      minsToTime(eveningEndMin),
			SlotType: "evening",
		})
	}

	return slots
}

// buildArrivalDay: check-in → 1 light attraction → dinner.
// arrivalTimeStr is "HH:MM"; defaults to 15:00 if empty or unparseable.
func buildArrivalDay(places, food []SlotPlace, comboIncludesLunch bool, arrivalTimeStr string) []TimeSlot {
	slots := []TimeSlot{}
	cur := defaultArrivalStart // 15:00
	if arrivalTimeStr != "" {
		if parsed := timeToMinsFromStr(arrivalTimeStr); parsed >= 0 {
			cur = parsed
		}
	}
	foodIdx := 0

	// One afternoon activity (capped at 2h so we finish by 17:30)
	if len(places) > 0 {
		p := places[0]
		dur := activityDuration(p)
		if dur > 120 {
			dur = 120 // limit arrival day activity
		}
		end := cur + dur
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      minsToTime(end),
			SlotType: "afternoon_activity",
			Place:    &p,
		})
		cur = end
	}

	// Buffer
	slots = append(slots, TimeSlot{
		Start:    minsToTime(cur),
		End:      minsToTime(cur + bufferBetweenMin),
		SlotType: "buffer",
		IsBuffer: true,
	})

	// Dinner
	if foodIdx < len(food) {
		f := food[foodIdx]
		foodIdx++
		slots = append(slots, TimeSlot{
			Start:    minsToTime(dinnerStartMin),
			End:      minsToTime(dinnerEndMin),
			SlotType: "dinner",
			Place:    &f,
		})
	}

	// Evening
	slots = append(slots, TimeSlot{
		Start:    minsToTime(dinnerEndMin),
		End:      minsToTime(eveningEndMin),
		SlotType: "evening",
	})

	return slots
}

// buildDepartureDay: breakfast → 1 light morning activity (if any) → buffer → checkout
func buildDepartureDay(places, food []SlotPlace) []TimeSlot {
	slots := []TimeSlot{}
	cur := defaultLastDayStart
	foodIdx := 0

	// Breakfast
	if foodIdx < len(food) {
		f := food[foodIdx]
		foodIdx++
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      minsToTime(cur + 60),
			SlotType: "breakfast",
			Place:    &f,
		})
		cur += 60
	}

	// Morning activity (capped at 2.5h so done by ~10:30 at latest from 08:00)
	if len(places) > 0 {
		p := places[0]
		dur := activityDuration(p)
		if dur > 150 {
			dur = 150 // limit departure day activity to 2.5h
		}
		end := cur + dur
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      minsToTime(end),
			SlotType: "morning_activity",
			Place:    &p,
		})
		cur = end
	} else {
		// Departure day fallback: generic light activity block (market/souvenirs)
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      minsToTime(cur + 90),
			SlotType: "morning_activity",
		})
		cur += 90
	}

	// Final buffer before checkout/travel
	slots = append(slots, TimeSlot{
		Start:    minsToTime(cur),
		End:      minsToTime(cur + 30),
		SlotType: "buffer",
		IsBuffer: true,
	})

	return slots
}

// buildFullDay: breakfast → full-day attraction (actual duration) → optional secondary → dinner at 17:30
func buildFullDay(places, food []SlotPlace, comboIncludesLunch bool) []TimeSlot {
	slots := []TimeSlot{}
	foodIdx := 0
	cur := 8 * 60 // 08:00

	// Breakfast (07:00-07:30)
	if foodIdx < len(food) {
		f := food[foodIdx]
		foodIdx++
		slots = append(slots, TimeSlot{
			Start:    "07:00",
			End:      "07:30",
			SlotType: "breakfast",
			Place:    &f,
		})
	}

	// Primary full-day activity — use actual duration, cap at 17:00
	if len(places) > 0 {
		p := places[0]
		dur := activityDuration(p)
		end := cur + dur
		if end > 17*60 {
			end = 17 * 60
		}
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      minsToTime(end),
			SlotType: "full_day_activity",
			Place:    &p,
		})
		cur = end + bufferBetweenMin

		// If primary ends before 15:30 and there's a short secondary place, add it
		if cur < 15*60+30 && len(places) > 1 {
			p2 := places[1]
			dur2 := activityDuration(p2)
			end2 := cur + dur2
			if end2 <= 17*60 && !p2.IsFullDay {
				slots = append(slots, TimeSlot{
					Start:    minsToTime(cur),
					End:      minsToTime(end2),
					SlotType: "afternoon_activity",
					Place:    &p2,
				})
				cur = end2
			}
		}
	}

	// Buffer before dinner (17:30)
	const fullDayDinnerStart = 17*60 + 30
	if cur < fullDayDinnerStart {
		slots = append(slots, TimeSlot{
			Start:    minsToTime(cur),
			End:      "17:30",
			SlotType: "buffer",
			IsBuffer: true,
		})
	}

	// Dinner at 17:30
	if foodIdx < len(food) {
		f := food[foodIdx]
		foodIdx++
		slots = append(slots, TimeSlot{
			Start:    minsToTime(fullDayDinnerStart),
			End:      minsToTime(fullDayDinnerStart + 90),
			SlotType: "dinner",
			Place:    &f,
		})
	}

	return slots
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func determineDayType(dayNum, totalDays int, places []SlotPlace) string {
	if dayNum == 1 {
		return "arrival"
	}
	if dayNum == totalDays {
		return "departure"
	}
	for _, p := range places {
		if p.IsFullDay {
			return "full_day"
		}
	}
	return "standard"
}

func computeDate(startDate string, offset int) string {
	t, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return startDate
	}
	d := t.AddDate(0, 0, offset)
	days := []string{"CN", "T2", "T3", "T4", "T5", "T6", "T7"}
	weekday := days[d.Weekday()]
	return fmt.Sprintf("%s %02d/%02d/%d", weekday, d.Day(), int(d.Month()), d.Year())
}

func computeTravelMin(slots []TimeSlot) int {
	total := 0
	var prevPlace *SlotPlace
	for i := range slots {
		s := &slots[i]
		if s.Place == nil || !activitySlotTypes[s.SlotType] {
			continue
		}
		if prevPlace != nil && prevPlace.Lat != 0 && s.Place.Lat != 0 {
			km := HaversineKm(prevPlace.Lat, prevPlace.Lng, s.Place.Lat, s.Place.Lng)
			total += EstimateTravelMin(km)
		}
		prevPlace = s.Place
	}
	return total
}

func computeBufferMin(slots []TimeSlot) int {
	total := 0
	for _, s := range slots {
		if s.IsBuffer {
			total += 30
		}
	}
	return total
}

var activitySlotTypes = map[string]bool{
	"morning_activity":   true,
	"afternoon_activity": true,
	"full_day_activity":  true,
	"evening_activity":   true,
}

var mealSlotTypes = map[string]bool{
	"breakfast": true,
	"lunch":     true,
	"dinner":    true,
}

// closingHourMin parses "HH:MM-HH:MM" and returns the closing time in minutes.
// Returns 24*60 if format is unrecognized.
func closingHourMin(hours string) int {
	if len(hours) < 11 {
		return 24 * 60
	}
	parts := strings.Split(hours, "-")
	if len(parts) != 2 {
		return 24 * 60
	}
	t := strings.TrimSpace(parts[1])
	var h, m int
	if _, err := fmt.Sscanf(t, "%d:%d", &h, &m); err != nil {
		return 24 * 60
	}
	return h*60 + m
}

// timeToMinsFromStr converts "HH:MM" to minutes since midnight.
// Returns -1 if the string is empty or unparseable.
func timeToMinsFromStr(s string) int {
	if s == "" {
		return -1
	}
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return -1
	}
	return h*60 + m
}

