package reminder

import (
	"context"
	"testing"
	"time"
)

// TestCancelledInFlightLeaseExpiryStaysCancelled ensures a user cancellation
// during a stalled send is never revived when the lease later expires.
func TestCancelledInFlightLeaseExpiryStaysCancelled(t *testing.T) {
	f := setup(t, true)
	f.due()
	if err := f.store.ScanReminderJobs(context.Background(), f.now, 24*time.Hour, f.scheduler.allowed); err != nil {
		t.Fatal(err)
	}
	leased, err := f.store.ClaimReminder(context.Background(), f.now, 24*time.Hour, f.scheduler.allowed, "in-flight")
	if err != nil || leased == nil {
		t.Fatalf("claim %v %v", leased, err)
	}
	if err = f.svc.CancelJob(context.Background(), f.user.ID, leased.ID); err != nil {
		t.Fatal(err)
	}
	f.scan(t)
	if f.sender.calls != 0 {
		t.Fatal("cancelled job sent while lease live")
	}
	f.now = f.now.Add(61 * time.Second)
	f.scan(t)
	job := f.jobs(t)[0]
	if job.Status != "cancelled" || job.AttemptToken != nil || job.LockedUntil != nil {
		t.Fatalf("cancelled lease recovered: %+v", job)
	}
	if f.sender.calls != 0 {
		t.Fatal("cancelled job sent after lease expiry")
	}
}
