// Package main is the independent, explicit API-Key delivery controller.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/miclle/routex/internal/deliverycontroller"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "deliver" && args[0] != "replace" && args[0] != "retry-report" {
		return errors.New("usage: routex-controller {deliver|replace|retry-report} -config /absolute/controller.json")
	}
	flags := flag.NewFlagSet("routex-controller "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "protected JSON bootstrap configuration")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = fmt.Fprintln(output, "Usage: routex-controller {deliver|replace|retry-report} -config /absolute/controller.json\nCredentials are read only from protected files. No polling or application reload is performed.")
			return err
		}
		// flag errors echo supplied values; never print arbitrary arguments.
		return errors.New("invalid controller command arguments")
	}
	if *configPath == "" || flags.NArg() != 0 {
		return errors.New("a protected configuration file is required")
	}
	cfg, err := deliverycontroller.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	var result deliverycontroller.Result
	switch args[0] {
	case "deliver":
		result, err = deliverycontroller.Deliver(ctx, cfg)
	case "replace":
		result, err = deliverycontroller.Replace(ctx, cfg)
	default:
		result, err = deliverycontroller.RetryReport(ctx, cfg)
	}
	if result.ReportRecorded {
		if _, writeErr := fmt.Fprintf(output, "controller report recorded (agent=%s, output=%s)\n", result.AgentState, result.OutputState); writeErr != nil {
			return errors.New("write controller result failed")
		}
	}
	return err
}
