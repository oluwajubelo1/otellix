package otellix

import (
	"testing"
	"time"
)

// TestGetForecastInUsesTheGivenZone checks that the forecast's month starts
// in the given time zone, not the server's.
func TestGetForecastInUsesTheGivenZone(t *testing.T) {
	ResetCallRecords()
	t.Cleanup(ResetCallRecords)
	lagos, err := time.LoadLocation("Africa/Lagos") // UTC+1, no daylight saving
	if err != nil {
		t.Skip("no time zone database")
	}
	now := time.Now()
	if now.In(lagos).Month() != now.UTC().Month() {
		t.Skip("Lagos and UTC are in different months for this hour")
	}

	// Lagos's month starts an hour before UTC's. Spend 30 minutes before
	// the UTC month started is this month in Lagos, but last month in UTC.
	utcMonthStart := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	recordCall(CallRecord{Timestamp: utcMonthStart.Add(-30 * time.Minute), CostUSD: 1})
	recordCall(CallRecord{Timestamp: now, CostUSD: 2})

	if got := GetForecast("").CurrentMonthSpend; got != 2 {
		t.Errorf("UTC month: expected $2 of spend, got $%.2f", got)
	}
	if got := GetForecastIn("", lagos).CurrentMonthSpend; got != 3 {
		t.Errorf("Lagos month: expected $3 of spend, got $%.2f", got)
	}
}
