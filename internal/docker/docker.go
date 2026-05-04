package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type RunOptions struct {
	Feature   string
	ImageRef  string
	Role      string
	Version   int
	Hash      string
	IsManaged bool
}

func GetContainerName(feature, role, hash string) string {
	// Replace underscores in feature with hyphens
	f := strings.ReplaceAll(feature, "_", "-")
	return fmt.Sprintf("builder_%s_%s_%s", f, role, hash)
}

func RunVersionedContainer(opts RunOptions) error {
	containerName := GetContainerName(opts.Feature, opts.Role, opts.Hash)

	// Check if container exists
	inspectCmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", containerName)
	output, err := inspectCmd.Output()
	if err == nil {
		running := strings.TrimSpace(string(output))
		if running == "true" {
			fmt.Printf("Container %s is already running\n", containerName)
			return nil
		}
		// Exists but not running, remove it
		fmt.Printf("Removing stopped container %s\n", containerName)
		exec.Command("docker", "rm", containerName).Run()
	}

	// Run container
	args := []string{"run", "-d", "--name", containerName}
	
	// Labels
	args = append(args, "--label", fmt.Sprintf("builder.feature=%s", opts.Feature))
	args = append(args, "--label", fmt.Sprintf("builder.hash=%s", opts.Hash))
	args = append(args, "--label", fmt.Sprintf("builder.role=%s", opts.Role))
	if opts.Version > 0 {
		args = append(args, "--label", fmt.Sprintf("builder.version=%d", opts.Version))
	}
	if opts.IsManaged {
		args = append(args, "--label", "builder.managed=true")
	}

	args = append(args, opts.ImageRef)

	fmt.Printf("Starting container %s from %s\n", containerName, opts.ImageRef)
	runCmd := exec.Command("docker", args...)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	return runCmd.Run()
}

func StopAndRemoveContainer(name string) error {
	fmt.Printf("Stopping and removing container %s\n", name)
	exec.Command("docker", "stop", name).Run()
	return exec.Command("docker", "rm", name).Run()
}

func StopContainersByFeatureRole(feature, role string) error {
	filter := fmt.Sprintf("label=builder.feature=%s", feature)
	if role != "" {
		filter += fmt.Sprintf(",label=builder.role=%s", role)
	}

	cmd := exec.Command("docker", "ps", "-a", "-q", "--filter", filter)
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	ids := strings.Fields(string(output))
	for _, id := range ids {
		fmt.Printf("Stopping and removing container %s (feature=%s, role=%s)\n", id, feature, role)
		exec.Command("docker", "stop", id).Run()
		exec.Command("docker", "rm", id).Run()
	}
	return nil
}

func GCStaleContainers(feature string, keepN int) error {
	// List containers for feature, exclude role=current
	filter := fmt.Sprintf("label=builder.feature=%s", feature)
	// We'll filter in Go to exclude current role
	cmd := exec.Command("docker", "ps", "-a", "--filter", filter, "--format", "{{.ID}}|{{.Label \"builder.role\"}}|{{.CreatedAt}}")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		fmt.Println("No containers found for GC")
		return nil
	}

	type contInfo struct {
		id   string
		role string
		created string
	}
	var candidates []contInfo
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		if parts[1] == "current" {
			continue
		}
		candidates = append(candidates, contInfo{id: parts[0], role: parts[1], created: parts[2]})
	}

	// docker ps -a returns by creation time descending by default
	// so we just skip the first keepN
	if len(candidates) <= keepN {
		fmt.Printf("Keeping all %d non-current containers (limit %d)\n", len(candidates), keepN)
		return nil
	}

	toRemove := candidates[keepN:]
	fmt.Printf("GC: Removing %d stale containers, keeping latest %d\n", len(toRemove), keepN)
	for _, c := range toRemove {
		fmt.Printf("GC: Removing %s (role=%s, created=%s)\n", c.id, c.role, c.created)
		exec.Command("docker", "stop", c.id).Run()
		exec.Command("docker", "rm", c.id).Run()
	}

	return nil
}
