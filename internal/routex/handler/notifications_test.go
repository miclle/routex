package handler

import "testing"

func TestNotificationFilter(t *testing.T) {
	filter, err := notificationFilter("status=unread&severity=high&cursor=ntf_01&limit=25")
	if err != nil {
		t.Fatal(err)
	}
	if !filter.UnreadOnly || filter.Severity != "high" || filter.Cursor != "ntf_01" || filter.Limit != 25 {
		t.Fatalf("unexpected filter: %+v", filter)
	}
	for _, query := range []string{"unknown=value", "limit=nope", "limit=1&limit=2", "status=read", "unread_only=true", "severity="} {
		if _, err := notificationFilter(query); err == nil {
			t.Fatalf("query %q was accepted", query)
		}
	}
}

func TestOperationalAlertFilter(t *testing.T) {
	filter, err := operationalAlertFilter("state=open&severity=medium&cursor=alr_01&limit=40")
	if err != nil {
		t.Fatal(err)
	}
	if filter.State != "open" || filter.Severity != "medium" || filter.Cursor != "alr_01" || filter.Limit != 40 {
		t.Fatalf("unexpected filter: %+v", filter)
	}
	for _, query := range []string{"unknown=value", "limit=nope", "limit=1&limit=2", "state="} {
		if _, err := operationalAlertFilter(query); err == nil {
			t.Fatalf("query %q was accepted", query)
		}
	}
}
