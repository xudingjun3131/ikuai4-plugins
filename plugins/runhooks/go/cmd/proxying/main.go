// Command proxying is the 巡流 NetLoom gateway traffic-observation engine entrypoint.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GitterBois/runhooks/internal/app"
	"github.com/GitterBois/runhooks/internal/config"
)

// Version is overridden at link time in release builds (-ldflags "-X main.Version=...").
var Version = "0.0.0-dev"

const shutdownTimeout = 10 * time.Second

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	fs := flag.NewFlagSet("proxying", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg, err := config.Parse(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		log.Error("config", "err", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println(Version)
		return 0
	}

	if cfg.MCPStdio {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	a, err := app.New(cfg, log, Version)
	if err != nil {
		if cfg.MCPStdio {
			fmt.Fprintf(os.Stderr, "proxying mcp: %v\n", err)
		} else {
			log.Error("app", "err", err)
		}
		return 1
	}
	if cfg.MCPStdio {
		if err := a.MCPServer().RunStdio(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "proxying mcp: %v\n", err)
			return 1
		}
		return 0
	}
	a.Start()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// SIGHUP: 热重载域名拦截范围(service.sh 改完 scope 文件后发, 抓包不中断)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			a.ReloadScope()
		}
	}()

	if err := a.Wait(ctx); err != nil {
		log.Error("server failed", "err", err)
		a.Shutdown(shutdownTimeout)
		return 1
	}
	log.Info("shutdown", "reason", ctx.Err().Error())
	a.Shutdown(shutdownTimeout)
	return 0
}
