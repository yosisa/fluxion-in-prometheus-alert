package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yosisa/fluxion/message"
	"github.com/yosisa/fluxion/plugin"
)

const apiPath = "/api/alerts"

type Config struct {
	Tag  string
	Bind string
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
	http.Handle(apiPath, newAlertHandler(p.env, p.conf.Tag))
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
	env *plugin.Env
	tag string
}

func newAlertHandler(env *plugin.Env, tag string) *alertHandler {
	return &alertHandler{
		env: env,
		tag: tag,
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
		h.env.Emit(message.NewEvent(h.tag, alert))
	}
}

func main() {
	plugin.New("in-prometheus-alert", func() plugin.Plugin {
		return &InPrometheusAlert{}
	}).Run()
}
