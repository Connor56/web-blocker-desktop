package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"annoying-web-blocker-desktop/internal/config"
	"annoying-web-blocker-desktop/internal/daemon"
	"annoying-web-blocker-desktop/internal/hosts"
	"annoying-web-blocker-desktop/internal/installer"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "annoying-web-blocker:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "--daemon", "daemon":
			return runDaemon()
		case "--install-root":
			return runRootInstall(args[1:])
		case "--uninstall-root":
			return installer.UninstallRoot()
		case "install":
			if err := installer.Install(); err != nil {
				return err
			}
			return installer.OpenUI()
		case "open":
			return installer.OpenUI()
		case "uninstall":
			return installer.Uninstall()
		case "help", "--help", "-h":
			printUsage()
			return nil
		default:
			return fmt.Errorf("unknown command %q (use --help)", args[0])
		}
	}

	if !installer.IsInstalled() {
		if err := installer.Install(); err != nil {
			return err
		}
	}
	return installer.OpenUI()
}

func runDaemon() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return daemon.RunService(ctx, daemon.Options{
		ConfigPath: config.DefaultPath,
		TokenPath:  installer.SystemTokenPath,
		HostsPath:  hosts.DefaultPath,
	})
}

func runRootInstall(args []string) error {
	flags := flag.NewFlagSet("--install-root", flag.ContinueOnError)
	source := flags.String("source", "", "source executable")
	home := flags.String("user-home", "", "installing user's home directory")
	uidText := flags.String("uid", "", "installing user's numeric UID")
	gidText := flags.String("gid", "", "installing user's numeric GID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	uid, err := strconv.Atoi(*uidText)
	if err != nil {
		return fmt.Errorf("invalid UID: %w", err)
	}
	gid, err := strconv.Atoi(*gidText)
	if err != nil {
		return fmt.Errorf("invalid GID: %w", err)
	}
	return installer.InstallRoot(*source, *home, uid, gid)
}

func printUsage() {
	fmt.Println(`Annoying Web Blocker Desktop

Usage:
  annoying-web-blocker-desktop             Install if needed, then open the UI
  annoying-web-blocker-desktop install     Install or update, then open the UI
  annoying-web-blocker-desktop open        Open the installed UI
  annoying-web-blocker-desktop uninstall   Remove the daemon and managed hosts entries`)
}
