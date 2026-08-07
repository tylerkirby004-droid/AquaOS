package state

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

func testValue(t *testing.T, plane Plane, now time.Time) Value {
	t.Helper()
	id, err := domain.NewEquipmentID()
	if err != nil {
		t.Fatal(err)
	}
	source, err := domain.NewEndpointID()
	if err != nil {
		t.Fatal(err)
	}
	return Value{Key: Key{EntityKind: EntityEquipment, EntityID: domain.EntityID(id), Plane: plane, Attribute: "enabled"}, Value: domain.NewBooleanValue(true), Quality: domain.QualityGood, ObservedAt: now, ReceivedAt: now, FreshFor: time.Minute, Source: source}
}

func TestDesiredAndReportedStateRemainSeparate(t *testing.T) {
	now := time.Now().UTC()
	manager := NewManager(nil, WithClock(func() time.Time { return now }))
	desired := testValue(t, PlaneDesired, now)
	reported := desired
	reported.Key.Plane = PlaneReported
	reported.Value = domain.NewBooleanValue(false)
	if _, err := manager.Set(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Set(context.Background(), reported); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 2 || len(snapshot.Values) != 2 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestRevisionsAreMonotonicAndRaceSafe(t *testing.T) {
	now := time.Now().UTC()
	manager := NewManager(nil)
	base := testValue(t, PlaneReported, now)
	const count = 100
	revisions := make(chan domain.Revision, count)
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			value := base
			value.Key.Attribute = string(rune('a'+index%26)) + time.Duration(index).String()
			stored, err := manager.Set(context.Background(), value)
			if err != nil {
				t.Error(err)
				return
			}
			revisions <- stored.Revision
		}(index)
	}
	wait.Wait()
	close(revisions)
	seen := make(map[domain.Revision]struct{}, count)
	for revision := range revisions {
		seen[revision] = struct{}{}
	}
	snapshot, err := manager.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != count || snapshot.Revision != count {
		t.Fatalf("revisions=%d snapshot=%d", len(seen), snapshot.Revision)
	}
}

func TestSlowSubscriberCannotBlockAndReceivesLatest(t *testing.T) {
	now := time.Now().UTC()
	manager := NewManager(nil)
	subscription, err := manager.Subscribe(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	value := testValue(t, PlaneReported, now)
	for index := 0; index < 50; index++ {
		value.Key.Attribute = time.Duration(index).String()
		if _, err := manager.Set(context.Background(), value); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case update := <-subscription.Updates():
		if update.Snapshot.Revision != 50 || update.Dropped == 0 {
			t.Fatalf("update=%+v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked core")
	}
}

func TestFreshnessAndSnapshotImmutability(t *testing.T) {
	now := time.Now().UTC()
	manager := NewManager(nil, WithClock(func() time.Time { return now.Add(2 * time.Minute) }))
	value := testValue(t, PlaneReported, now)
	stored, err := manager.Set(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Values[0].Quality != domain.QualityStale {
		t.Fatal("snapshot did not evaluate freshness")
	}
	*snapshot.Values[0].Value.Boolean = false
	current, err := manager.Get(context.Background(), stored.Key)
	if err != nil {
		t.Fatal(err)
	}
	if current.Quality != domain.QualityStale || !*current.Value.Boolean {
		t.Fatalf("current=%+v", current)
	}
}
