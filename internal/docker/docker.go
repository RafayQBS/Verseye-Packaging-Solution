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
