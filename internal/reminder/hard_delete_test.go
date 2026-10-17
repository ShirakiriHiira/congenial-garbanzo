package reminder

import (
	"context"
	"testing"
)

func TestDeletedLeasedReminderIsNeverSent(t *testing.T) {
	for _, card := range []bool{false, true} {
		name := "period"
		if card {
			name = "card"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t, true)
			f.due()
			ctx := context.Background()
			if err := f.store.ScanReminderJobs(ctx, f.now, f.scheduler.window, f.scheduler.allowed); err != nil {
				t.Fatal(err)
			}
			job, err := f.store.ClaimReminder(ctx, f.now, f.scheduler.window, f.scheduler.allowed, "delete-lease")
			if err != nil || job == nil {
				t.Fatalf("claim: %+v %v", job, err)
			}
			if card {
				err = f.svc.DeleteSIM(ctx, f.user.ID, f.sim.ID)
			} else {
				err = f.svc.DeletePeriod(ctx, f.user.ID, f.period.ID, f.period.Cycle)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.scheduler.deliver(ctx, job); err != nil {
				t.Fatal(err)
			}
			f.scan(t)
			if f.sender.calls != 0 || len(f.jobs(t)) != 0 {
				t.Fatalf("deleted reminder sent/regenerated: sends=%d jobs=%+v", f.sender.calls, f.jobs(t))
			}
		})
	}
}
