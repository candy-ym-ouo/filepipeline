package domain

import "testing"

func TestStateMachine(t *testing.T) {
	cases := []struct {
		from, to Status
		ok       bool
	}{
		{StatusPending, StatusProcessing, true},
		{StatusProcessing, StatusPending, true},
		{StatusProcessing, StatusSucceeded, true},
		{StatusFailed, StatusPending, true},
		{StatusSucceeded, StatusPending, false},
		{StatusDead, StatusPending, false},
	}
	for _, tc := range cases {
		if got := CanTransition(tc.from, tc.to); got != tc.ok {
			t.Fatalf("CanTransition(%s,%s)=%t want %t", tc.from, tc.to, got, tc.ok)
		}
	}
	if next, _ := StageValidate.Next(); next != StageExtract {
		t.Fatalf("next=%s", next)
	}
	if !StatusSucceeded.Final() || StatusFailed.Final() {
		t.Fatal("final state classification is wrong")
	}
}

func TestNewTaskDefaults(t *testing.T) {
	task, err := NewTask(NewTaskInput{Filename: "a.txt", StoredName: "f_a.txt", Size: 1, SHA256: "x", MIME: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusPending || task.Stage != StageValidate || task.MaxAttempts != 3 {
		t.Fatalf("unexpected defaults: %+v", task)
	}
	if len(task.ID) < 3 || task.ID[:2] != "t_" {
		t.Fatalf("bad id %q", task.ID)
	}
}
