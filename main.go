package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type ContainerInfo struct {
	ID     string
	Name   string
	State  string
	Status string
	Ports  string
	Uptime time.Duration
	Raw    container.Summary
}

func main() {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		fmt.Println("Docker client error:", err)
		os.Exit(1)
	}
	defer cli.Close()

	useCompose := false
	if len(os.Args) > 1 && os.Args[1] == "--compose" {
		useCompose = true
	}

	var projectName string
	if useCompose {
		projectName = os.Getenv("COMPOSE_PROJECT_NAME")
		if projectName == "" {
			dir, errWd := os.Getwd()
			if errWd != nil {
				fmt.Println("Cannot determine current directory:", errWd)
				os.Exit(1)
			}
			projectName = filepath.Base(dir)
		}
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		fmt.Println("Error listing containers:", err)
		os.Exit(1)
	}

	uniqID := make(map[string]int)

	var infos []ContainerInfo
	for _, c := range containers {
		uniqID = fillUniqID(uniqID, c.ID)

		if useCompose && c.Labels["com.docker.compose.project"] != projectName {
			continue
		}
		inspect, err := cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}
		startedAt, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt)
		if err != nil {
			continue
		}
		uptime := time.Since(startedAt)

		portsMap := make(map[uint16]uint16, len(c.Ports))
		for _, p := range c.Ports {
			if p.PublicPort != 0 {
				portsMap[p.PublicPort] = p.PrivatePort
			} else {
				portsMap[p.PrivatePort] = 0
			}
		}

		keys := make([]uint16, 0, len(portsMap))
		for k := range portsMap {
			keys = append(keys, k)
		}

		slices.SortFunc(keys, func(a, b uint16) int {
			return int(a) - int(b)
		})

		ports := make([]string, 0, len(portsMap))

		for _, port := range keys {
			if portsMap[uint16(port)] != 0 {
				ports = append(ports, fmt.Sprintf("%d→%d", port, portsMap[uint16(port)]))
			} else {
				ports = append(ports, fmt.Sprintf("%d", port))
			}
		}

		info := ContainerInfo{
			ID:     c.ID,
			Name:   strings.TrimPrefix(c.Names[0], "/"),
			State:  c.State,
			Status: humanUptime(uptime),
			Ports:  strings.Join(ports, ", "),
			Uptime: uptime,
			Raw:    c,
		}
		infos = append(infos, info)
	}

	slices.SortFunc(infos, func(a, b ContainerInfo) int {
		return strings.Compare(a.Name, b.Name)
	})

	biggestName := 0
	for _, info := range infos {
		if len(info.Name) > biggestName {
			biggestName = len(info.Name)
		}
	}

	blue := "\033[34m"
	green := "\033[32m"
	yellow := "\033[38;05;214m"
	red := "\033[31m"
	reset := "\033[0m"
	brightBlack := "\033[90m"

	pattern := "%s%-12s %-" + fmt.Sprintf("%d", biggestName+3) + "s %-11s %-10s %s%s\n"

	fmt.Printf(pattern, blue, "ID", "NAME", "STATE", "STATUS", "PORTS", reset)
	for _, info := range infos {
		colMark := green
		if info.Uptime < time.Minute {
			colMark = yellow
		}

		if info.Uptime < time.Second*10 {
			colMark = red
		}

		if info.State != "running" {
			colMark = red
		}

		offset := findUniqIdOffset(uniqID, info.ID)
		id := fmt.Sprintf("%s%s%s%s%s", reset, info.ID[0:offset], brightBlack, info.ID[offset:12], reset)

		stateMark := fmt.Sprintf("%s●%s", colMark, reset)
		pattern = "%s %-" + fmt.Sprintf("%d", biggestName+3) + "s %s %-9s %-10s %s\n"
		fmt.Printf(pattern, id, info.Name, stateMark, info.State, info.Status, info.Ports)
	}
}

func fillUniqID(uniqId map[string]int, containerID string) map[string]int {
	for i := range min(len(containerID), 10) {
		key := containerID[:i+1]
		if count, ok := uniqId[key]; ok {
			uniqId[key] = count + 1
			continue
		}

		uniqId[key] = 1
	}

	return uniqId
}

func findUniqIdOffset(uniqId map[string]int, containerID string) int {
	var key string
	for i := range min(len(containerID), 10) {
		key = containerID[:i+1]
		count := uniqId[key]

		if count == 1 {
			break
		}
	}

	return len(key)
}

func humanUptime(d time.Duration) string {
	if d.Hours() >= 24 {
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		if hours > 0 {
			return fmt.Sprintf("Up %dd%dh", days, hours)
		}
		return fmt.Sprintf("Up %dd", days)
	}
	if d.Hours() >= 1 {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		if minutes > 0 {
			return fmt.Sprintf("Up %dh%dm", hours, minutes)
		}
		return fmt.Sprintf("Up %dh", hours)
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	if minutes > 0 {
		return fmt.Sprintf("Up %dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("Up %ds", int(d.Seconds()))
}
