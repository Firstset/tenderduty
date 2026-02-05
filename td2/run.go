package tenderduty

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	dash "github.com/firstset/tenderduty/v2/td2/dashboard"
)

var td = &Config{}

func Run(configFile, stateFile, chainConfigDirectory string, password *string, devMode bool) error {
	var err error
	td, err = loadConfig(configFile, stateFile, chainConfigDirectory, password)
	if err != nil {
		return err
	}
	fatal, problems := validateConfig(td)
	for _, p := range problems {
		fmt.Println(p)
	}
	if fatal {
		slog.Error("tenderduty configuration is invalid, refusing to start")
		os.Exit(1)
	}
	slog.Info("tenderduty config is valid, starting tenderduty", "chains", len(td.Chains))

	defer td.cancel()

	go func() {
		for {
			select {
			case alert := <-td.alertChan:
				go func(msg *alertMsg) {
					var e error
					e = notifyPagerduty(msg)
					if e != nil {
						l(slog.LevelWarn, msg.chain, "error sending alert to pagerduty", e.Error())
					}
					e = notifyDiscord(msg)
					if e != nil {
						l(slog.LevelWarn, msg.chain, "error sending alert to discord", e.Error())
					}
					e = notifyTg(msg)
					if e != nil {
						l(slog.LevelWarn, msg.chain, "error sending alert to telegram", e.Error())
					}
					e = notifySlack(msg)
					if e != nil {
						l(slog.LevelWarn, msg.chain, "error sending alert to slack", e.Error())
					}
					e = notifyWebhook(msg)
					if e != nil {
						l(slog.LevelWarn, msg.chain, "error sending alert to webhook", e.Error())
					}
				}(alert)
			case <-td.ctx.Done():
				return
			}
		}
	}()

	if td.EnableDash {
		go dash.Serve(td.Listen, td.updateChan, td.logChan, td.HideLogs, devMode)
		l(slog.LevelInfo, "starting dashboard on", td.Listen)
	} else {
		go func() {
			for {
				<-td.updateChan
			}
		}()
	}
	if td.Prom {
		go prometheusExporter(td.ctx, td.statsChan)
	} else {
		go func() {
			for {
				<-td.statsChan
			}
		}()
	}

	// tenderduty health checks:
	if td.Healthcheck.Enabled {
		td.pingHealthcheck()
	}

	for k := range td.Chains {
		cc := td.Chains[k]

		go func(cc *ChainConfig, name string) {
			// alert worker
			go cc.watch()

			// node health checks:
			go func() {
				for {
					cc.monitorHealth(td.ctx, name)
				}
			}()

			// websocket subscription and occasional validator info refreshes
			for {
				e := cc.newRpc()
				if e != nil {
					l(slog.LevelWarn, cc.ChainId, e)
					time.Sleep(5 * time.Second)
					continue
				}

				e = cc.GetMinSignedPerWindow()
				if e != nil {
					l(slog.LevelError, "🛑", cc.ChainId, e)
				}

				e = cc.GetValInfo(true)
				if e != nil {
					l(slog.LevelError, "🛑", cc.ChainId, e)
				}
				cc.WsRun()
				l(slog.LevelWarn, cc.ChainId, "🌀 websocket exited! Restarting monitoring")
				time.Sleep(5 * time.Second)
			}
		}(cc, k)
	}

	// attempt to save state on exit, only a best-effort ...
	saved := make(chan any)
	go saveOnExit(stateFile, saved)

	<-td.ctx.Done()
	<-saved

	return err
}

func saveOnExit(stateFile string, saved chan any) {
	quitting := make(chan os.Signal, 1)
	signal.Notify(quitting, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	saveState := func() {
		defer close(saved)
		slog.Info("saving state")
		//#nosec -- variable specified on command line
		f, e := os.OpenFile(stateFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if e != nil {
			slog.Error("failed to open state file for writing", "err", e)
			return
		}
		td.chainsMux.Lock()
		defer td.chainsMux.Unlock()
		blocks := make(map[string][]int)
		// only need to save counts if the dashboard exists
		if td.EnableDash {
			for k, v := range td.Chains {
				blocks[k] = v.blocksResults
			}
		}
		nodesDown := make(map[string]map[string]time.Time)
		for k, v := range td.Chains {
			for _, node := range v.Nodes {
				if node.down {
					if nodesDown[k] == nil {
						nodesDown[k] = make(map[string]time.Time)
					}
					nodesDown[k][node.Url] = node.downSince
				}
			}
		}
		b, e := json.Marshal(&savedState{
			Alarms:    alarms,
			Blocks:    blocks,
			NodesDown: nodesDown,
		})
		if e != nil {
			slog.Error("failed to marshal state", "err", e)
			return
		}
		_, _ = f.Write(b)
		_ = f.Close()
		slog.Info("tenderduty exiting")
	}
	for {
		select {
		case <-td.ctx.Done():
			saveState()
			return
		case <-quitting:
			saveState()
			td.cancel()
			return
		}
	}
}
