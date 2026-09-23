package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestSiteStrictBodies(t *testing.T) {
	for _, body := range []string{`{"content":"value","unknown":true}`, `{"content":"value"} {}`, `[]`} {
		router := fox.New()
		ctrl := &Ctrl{}
		router.POST("/announcements/:announcement_id", jsonManagementRequest, ctrl.UpdateAnnouncement)
		request := httptest.NewRequest("POST", "/announcements/ann_test", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatalf("body %s: status %d", body, response.Code)
		}
	}
}

func TestAnnouncementStrictQueries(t *testing.T) {
	for _, path := range []string{"/active?%zz", "/active?ignored", "/admin?cursor=%zz", "/admin?cursor=a&cursor=b", "/admin?cursor="} {
		router := fox.New()
		ctrl := &Ctrl{}
		router.GET("/active", ctrl.ActiveAnnouncements)
		router.GET("/admin", ctrl.AdminAnnouncements)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 400 {
			t.Fatalf("%s returned %d", path, response.Code)
		}
	}
}
