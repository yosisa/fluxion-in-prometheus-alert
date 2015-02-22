package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yosisa/fluxion/buffer"
	"github.com/yosisa/fluxion/message"
	"github.com/yosisa/fluxion/plugin"
)

const apiPath = "/api/alerts"

type Config struct {
	Tag       string
	Bind      string
	FirstOnly bool `toml:"first_only"`
	TTL       buffer.Duration
}

type InPrometheusAlert struct {
	env  *plugin.Env
	conf *Config
}

func (p *InPrometheusAlert) Init(env *plugin.Env) error {
	p.env = env
	p.conf = &Config{}
	return env.ReadConfig(p.conf)
}

func (p *InPrometheusAlert) Start() error {
	c := make(chan error, 1)
	http.Handle(apiPath, newAlertHandler(p.env, p.conf))
	go func() {
		c <- http.ListenAndServe(p.conf.Bind, nil)
	}()
	select {
	case err := <-c:
		return err
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func (p *InPrometheusAlert) Close() error {
	return nil
}

type alertHandler struct {
	env       *plugin.Env
	tag       string
	firstOnly bool
	ttl       time.Duration
	seen      map[string]*time.Timer
	m         sync.Mutex
}

func newAlertHandler(env *plugin.Env, conf *Config) *alertHandler {
	return &alertHandler{
		env:       env,
		tag:       conf.Tag,
		firstOnly: conf.FirstOnly,
		ttl:       time.Duration(conf.TTL),
		seen:      make(map[string]*time.Timer),
	}
}

func (h *alertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	alerts := make([]map[string]interface{}, 0)
	if err := json.NewDecoder(r.Body).Decode(&alerts); err != nil {
		h.env.Log.Info(err)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "%v", err)
		return
	}
	for _, alert := range alerts {
		seen := h.handleAlert(alert)
		if !h.firstOnly || h.firstOnly && !seen {
			h.env.Emit(message.NewEvent(h.tag+".active", alert))
		}
	}
}

func (h *alertHandler) handleAlert(alert map[string]interface{}) (seen bool) {
	var timer *time.Timer
	id := h.makeID(alert)
	h.m.Lock()
	defer h.m.Unlock()
	timer, seen = h.seen[id]
	if seen {
		if timer != nil {
			timer.Reset(h.ttl)
		}
		return
	}
	if h.ttl > 0 {
		timer = time.AfterFunc(h.ttl, func() {
			h.m.Lock()
			defer h.m.Unlock()
			h.env.Emit(message.NewEvent(h.tag+".inactive", alert))
			delete(h.seen, id)
		})
	}
	h.seen[id] = timer
	return
}

func (h *alertHandler) makeID(alert map[string]interface{}) string {
	labels := alert["Labels"].(map[string]interface{})
	keys := make([]string, len(labels))
	var i int
	for key := range labels {
		keys[i] = key
		i++
	}
	sort.Strings(keys)
	vals := make([]string, len(labels))
	i = 0
	for _, key := range keys {
		vals[i] = fmt.Sprintf("%v", labels[key])
		i++
	}
	return strings.Join(vals, ":")
}

func main() {
	plugin.New("in-prometheus-alert", func() plugin.Plugin {
		return &InPrometheusAlert{}
	}).Run()
}
