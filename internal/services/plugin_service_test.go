package services

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func TestPlugins_ListToggleAndDatabasesConfig(t *testing.T) {
	db := testutil.NewDB(t, &models.Setting{})
	svc := NewPluginService(db)

	list := func() []Plugin {
		c, w := newTestContext(http.MethodGet, "/plugins", "")
		svc.List(c)
		var out []Plugin
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	if ps := list(); len(ps) != 2 || ps[0].Installed || ps[0].Footprint == "" {
		t.Fatalf("plugins = %+v", ps)
	}

	put := func(id, body string) int {
		c, w := newTestContext(http.MethodPut, "/plugins/"+id, body)
		c.Params = gin.Params{{Key: "id", Value: id}}
		svc.SetInstalled(c)
		return w.Code
	}
	if code := put("databases", `{"installed":true}`); code != http.StatusNotImplemented {
		t.Errorf("installing from the Hub isn't built yet: %d", code)
	}
	if code := put("databases", `{"installed":true,"manual":true}`); code != http.StatusOK || !list()[0].Installed {
		t.Fatalf("mark a hand-installed plugin: %d", code)
	}
	if code := put("notifications", `{"installed":true,"manual":true}`); code != http.StatusConflict {
		t.Errorf("a coming-soon plugin can't be installed: %d", code)
	}
	if code := put("nope", `{"installed":true}`); code != http.StatusNotFound {
		t.Errorf("unknown plugin: %d", code)
	}

	cfg := func(body string) int {
		c, w := newTestContext(http.MethodPut, "/plugins/databases/config", body)
		svc.SetDatabases(c)
		return w.Code
	}
	if code := cfg(`{"databases":[{"name":"x","primary_endpoint":"not an endpoint"}]}`); code != http.StatusBadRequest {
		t.Errorf("bad endpoint: %d", code)
	}
	if code := cfg(`{"databases":[{"name":"gamer-ak","engine":"Supabase","primary_node":"abdullah","primary_endpoint":"10.101.0.4:54322","standby_node":"asadullahs-pc","standby_endpoint":"10.101.0.3:54332","projects":["gamer-ak-bot"]}]}`); code != http.StatusOK {
		t.Fatalf("save config: %d", code)
	}
	var got struct{ Databases []DatabaseEntry }
	json.Unmarshal(list()[0].Config, &got)
	if len(got.Databases) != 1 || got.Databases[0].StandbyNode != "asadullahs-pc" {
		t.Errorf("config = %+v", got)
	}
}

func TestPlugins_CustomAdder(t *testing.T) {
	db := testutil.NewDB(t, &models.Setting{})
	svc := NewPluginService(db)
	add := func(body string) int {
		c, w := newTestContext(http.MethodPost, "/plugins/custom", body)
		svc.AddCustom(c)
		return w.Code
	}
	if code := add(`{"id":"uptime-kuma","name":"Uptime Kuma","source":"https://github.com/louislam/uptime-kuma","footprint":"One container on a node you pick."}`); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	for body, want := range map[string]int{
		`{"id":"uptime-kuma","name":"Again","source":"https://x.example"}`: http.StatusConflict,
		`{"id":"databases","name":"Clash","source":"https://x.example"}`:   http.StatusConflict,
		`{"id":"x y","name":"Bad id","source":"https://x.example"}`:        http.StatusBadRequest,
		`{"id":"plain","name":"HTTP","source":"http://x.example"}`:         http.StatusBadRequest,
	} {
		if code := add(body); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	c, w := newTestContext(http.MethodGet, "/plugins", "")
	svc.List(c)
	var out []Plugin
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out) != 3 || !out[2].Custom || out[2].Installed {
		t.Fatalf("list = %+v", out)
	}
	c, w = newTestContext(http.MethodDelete, "/plugins/custom/uptime-kuma", "")
	c.Params = gin.Params{{Key: "id", Value: "uptime-kuma"}}
	svc.RemoveCustom(c)
	if w.Code != http.StatusOK {
		t.Errorf("remove: %d", w.Code)
	}
}
