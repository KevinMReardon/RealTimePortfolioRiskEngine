package alpaca

import (
	"context"
	"testing"
)

func TestNextActivitiesPageToken(t *testing.T) {
	partial := []ActivityRow{{ID: "a"}, {ID: "b"}}
	if nextActivitiesPageToken(partial, 100) != "" {
		t.Fatalf("expected empty when fewer results than page size")
	}
	full := []ActivityRow{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if got := nextActivitiesPageToken(full, 3); got != "c" {
		t.Fatalf("full page: NextPageToken want c, got %q", got)
	}
	if nextActivitiesPageToken(nil, 10) != "" {
		t.Fatal("nil slice should produce empty token")
	}
}

func TestRESTInterface(t *testing.T) {
	var _ REST = (*RESTClient)(nil)
	var _ REST = (*FakeREST)(nil)
}

func TestFakeREST_ListActivitiesPaging(t *testing.T) {
	f := &FakeREST{
		ActivitiesPages: []ActivitiesPage{
			{Activities: []ActivityRow{{ID: "1"}}, NextPageToken: "ignored-by-fake"},
			{Activities: []ActivityRow{{ID: "2"}}},
		},
	}
	p1, err := f.ListActivities(context.Background(), ListActivitiesRequest{})
	if err != nil || len(p1.Activities) != 1 || p1.Activities[0].ID != "1" {
		t.Fatalf("page1: %+v err=%v", p1, err)
	}
	p2, err := f.ListActivities(context.Background(), ListActivitiesRequest{})
	if err != nil || len(p2.Activities) != 1 || p2.Activities[0].ID != "2" {
		t.Fatalf("page2: %+v err=%v", p2, err)
	}
}

func TestRESTConfig_Validate(t *testing.T) {
	if err := (RESTConfig{}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	if err := (RESTConfig{KeyID: "k", SecretKey: "s", BaseURL: "https://paper-api.alpaca.markets"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
