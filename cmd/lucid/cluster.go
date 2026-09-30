package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/jobs"
)

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func runCluster(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "up":
		if err := cluster.Up(); err != nil {
			fail(err)
		}
		kc, _ := cluster.KubeconfigPath()
		fmt.Println("cluster up; kubeconfig:", kc)
	case "down":
		if err := cluster.Down(); err != nil {
			fail(err)
		}
		fmt.Println("cluster down")
	case "status":
		ok, kc, err := cluster.Status()
		if err != nil {
			fail(err)
		}
		state := "not running"
		if ok {
			state = "running"
		}
		fmt.Printf("cluster %s: %s\nkubeconfig: %s\n", cluster.Name, state, kc)
	default:
		fmt.Fprintln(os.Stderr, "usage: lucid cluster <up|down|status>")
		os.Exit(2)
	}
}

func runJob(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	ctx := context.Background()
	switch sub {
	case "hello", "list", "logs":
	default:
		fmt.Fprintln(os.Stderr, "usage: lucid job <hello|list|logs <name>>")
		os.Exit(2)
	}
	r, err := jobs.Connect()
	if err != nil {
		fail(err)
	}
	switch sub {
	case "hello":
		j, err := r.SubmitHello(ctx)
		if err != nil {
			fail(err)
		}
		fmt.Println(j.Name)
	case "list":
		list, err := r.List(ctx)
		if err != nil {
			fail(err)
		}
		fmt.Printf("%-24s %-12s %s\n", "NAME", "STATUS", "IMAGE")
		for _, j := range list {
			fmt.Printf("%-24s %-12s %s\n", j.Name, j.Status, j.Image)
		}
	case "logs":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			fmt.Fprintln(os.Stderr, "usage: lucid job logs <name>")
			os.Exit(2)
		}
		if err := r.Logs(ctx, args[1], os.Stdout); err != nil {
			fail(err)
		}
	}
}
