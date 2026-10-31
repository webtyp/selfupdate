package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"webtyp.com/selfupdate"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "serve":
		if len(os.Args) != 4 {
			usage()
			os.Exit(1)
		}
		repo, app := os.Args[2], os.Args[3]
		sup, token, err := selfupdate.OpenSupervisor(selfupdate.AppDir(app), repo, nil, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		go selfupdate.RunBackupLoop(context.Background(), sup, selfupdate.BackupInterval)
		if err := http.ListenAndServe(":"+selfupdate.SupervisorPort, sup.Handler(token)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "install":
		if len(os.Args) != 3 {
			usage()
			os.Exit(1)
		}
		in := bufio.NewReader(os.Stdin)
		cfg := selfupdate.InstallConfig{
			Repo:            os.Args[2],
			PublicKey:       envOrPrompt(in, "PUBLIC_KEY", "Publisher public key (update.pub, base64): "),
			GitHubToken:     envOrPrompt(in, "GITHUB_TOKEN", "GitHub read token: "),
			TunnelToken:     envOrPrompt(in, "TUNNEL_TOKEN", "Cloudflare Tunnel token: "),
			DNSToken:        envOrPrompt(in, "DNS_TOKEN", "Cloudflare DNS API token: "),
			LANAddress:      os.Getenv(selfupdate.EnvLANAddress),
			SupervisorImage: os.Getenv(selfupdate.EnvSupervisorImage),
		}
		st, err := selfupdate.Install(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(st.Version)
	default:
		usage()
		os.Exit(1)
	}
}

func envOrPrompt(in *bufio.Reader, key, prompt string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	fmt.Fprint(os.Stderr, prompt)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:\n  selfupdate install <owner/repo>\n  selfupdate serve <owner/repo> <app>")
}
