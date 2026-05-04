package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/gin-gonic/gin"
	"github.com/maisam9060/platform-api/internal/cache"
	"github.com/maisam9060/platform-api/internal/config"
	"github.com/maisam9060/platform-api/internal/docker"
	"github.com/maisam9060/platform-api/internal/versioning"
	"gopkg.in/yaml.v3"
)

// --- Harbor config ---
const (
	harborRegistry = "harbor.qbscocloud.net:30003"
	harborProject  = "verseye-project"
)

// harborImage returns the full Harbor image reference for a feature and hash tag.
// e.g. harbor.qbscocloud.net:30003/verseye-project/feature3:ab12cd34
func harborImage(featureName, tag string) string {
	return fmt.Sprintf("%s/%s/%s:%s", harborRegistry, harborProject, featureName, tag)
}

// harborLogin authenticates docker with Harbor using env vars
// HARBOR_USER and HARBOR_PASSWORD (or HARBOR_TOKEN for robot accounts).
func harborLogin() error {
	user := os.Getenv("HARBOR_USER")
	password := os.Getenv("HARBOR_PASSWORD")
	if password == "" {
		password = os.Getenv("HARBOR_TOKEN") // support robot account tokens
	}
	if user == "" || password == "" {
		return fmt.Errorf("HARBOR_USER and HARBOR_PASSWORD (or HARBOR_TOKEN) env vars must be set")
	}

	r, w, _ := os.Pipe()
	go func() {
		w.WriteString(password)
		w.Close()
	}()

	cmd := exec.Command("docker", "login", harborRegistry, "-u", user, "--password-stdin")
	cmd.Stdin = r
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker login to Harbor failed: %w", err)
	}
	fmt.Printf("Logged in to Harbor: %s\n", harborRegistry)
	return nil
}

// --- Hashing ---
func HashDir(dir string) (string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		file, err := os.Open(f)
		if err != nil {
			return "", err
		}
		defer file.Close()
		if _, err := io.Copy(h, file); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ComputeFeatureHash(feat *config.Feature, depHashes []string) (string, error) {
	h := sha256.New()
	h.Write([]byte(feat.Command))
	for _, input := range feat.Inputs {
		hash, err := HashDir(input)
		if err != nil {
			return "", err
		}
		h.Write([]byte(hash))
	}
	sort.Strings(depHashes)
	for _, dh := range depHashes {
		h.Write([]byte(dh))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- Dependency graph ---
func buildGraph(cfg *config.BuilderConfig) map[string][]string {
	graph := make(map[string][]string)
	for name, feat := range cfg.Features {
		graph[name] = feat.DependsOn
	}
	return graph
}

func topoSort(node string, graph map[string][]string, visited, temp map[string]bool, order *[]string) error {
	if temp[node] {
		return fmt.Errorf("cycle detected at feature: %s", node)
	}
	if visited[node] {
		return nil
	}
	temp[node] = true
	for _, dep := range graph[node] {
		if _, ok := graph[dep]; !ok {
			return fmt.Errorf("unknown dependency: %s", dep)
		}
		if err := topoSort(dep, graph, visited, temp, order); err != nil {
			return err
		}
	}
	temp[node] = false
	visited[node] = true
	*order = append(*order, node)
	return nil
}

// --- Build logic ---
func BuildFeature(featureName string) error {
	// Load YAML
	data, err := os.ReadFile("builder.yaml")
	if err != nil {
		return fmt.Errorf("reading YAML: %w", err)
	}

	var cfg config.BuilderConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	// Attach names
	for name, feat := range cfg.Features {
		feat.Name = name
	}

	graph := buildGraph(&cfg)
	visited := make(map[string]bool)
	temp := make(map[string]bool)
	var buildOrder []string
	if err := topoSort(featureName, graph, visited, temp, &buildOrder); err != nil {
		return fmt.Errorf("dependency error: %w", err)
	}

	fmt.Println("Build order:", buildOrder)

	// Login to Harbor once before any builds
	if err := harborLogin(); err != nil {
		return err
	}

	hashCache := make(map[string]string)
	for _, fname := range buildOrder {
		feat := cfg.Features[fname]

		// Collect dependency hashes
		var depHashes []string
		for _, dep := range feat.DependsOn {
			depHashes = append(depHashes, hashCache[dep])
		}

		// Compute current feature hash
		newHash, err := ComputeFeatureHash(feat, depHashes)
		if err != nil {
			return fmt.Errorf("hashing feature %s: %w", fname, err)
		}

		oldHash, err := cache.ReadFeatureHash(fname)
		if err == nil && oldHash == newHash {
			fmt.Println("SKIP", fname)
			hashCache[fname] = newHash
			continue
		}

		fmt.Println("BUILD", fname)

		shortHash := newHash[:8]
		localTag := fmt.Sprintf("%s:%s", fname, shortHash)
		remoteTag := harborImage(fname, shortHash)

		// 1. docker build — tag locally
		buildCmd := exec.Command("docker", "build", "-t", localTag, feat.Inputs[0])
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("docker build failed for %s: %w", fname, err)
		}

		// 2. docker tag — apply the full Harbor image reference
		tagCmd := exec.Command("docker", "tag", localTag, remoteTag)
		tagCmd.Stdout = os.Stdout
		tagCmd.Stderr = os.Stderr
		if err := tagCmd.Run(); err != nil {
			return fmt.Errorf("docker tag failed for %s: %w", fname, err)
		}
		fmt.Printf("Tagged: %s -> %s\n", localTag, remoteTag)

		// 3. docker push — push to Harbor
		pushCmd := exec.Command("docker", "push", remoteTag)
		pushCmd.Stdout = os.Stdout
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			return fmt.Errorf("docker push failed for %s: %w", fname, err)
		}
		fmt.Printf("Pushed to Harbor: %s\n", remoteTag)

		// 4. Update cache and version history only after a successful push
		cache.WriteFeatureHash(fname, newHash)
		hashCache[fname] = newHash

		versioning.AppendVersionRecord(fname, versioning.VersionRecord{
			FullTag:      remoteTag,
			ShortHash:    shortHash,
			InputHash:    newHash,
			Dependencies: feat.DependsOn,
			BuildCommand: feat.Command,
		})

		// 5. docker run — start a container from the Harbor image
		// containerName := fmt.Sprintf("%s_container", fname)
		// fmt.Println("Starting container:", containerName)
		// runCmd := exec.Command("docker", "run", "-d", "--name", containerName, remoteTag)
		// runCmd.Stdout = os.Stdout
		// runCmd.Stderr = os.Stderr
		// if err := runCmd.Run(); err != nil {
		// 	return fmt.Errorf("docker run failed for %s: %w", fname, err)
		// }
		// fmt.Printf("Container started: %s (image: %s)\n", containerName, remoteTag)
	}
	return nil
}

// --- CLI Actions ---
func ListVersions(featureName string, last int, asJSON bool) error {
	versions, err := versioning.LoadVersions(featureName)
	if err != nil {
		return err
	}

	if last > 0 && len(versions) > last {
		versions = versions[len(versions)-last:]
	}

	if asJSON {
		data, _ := json.MarshalIndent(versions, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	currentHash, _ := cache.ReadFeatureHash(featureName)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "VER\tTAG\tHASH\tCURRENT")
	for _, v := range versions {
		isCurrent := ""
		if v.InputHash == currentHash {
			isCurrent = "*"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", v.Version, v.FullTag, v.ShortHash, isCurrent)
	}
	w.Flush()
	return nil
}

func ResolveVersion(featureName string, version int, tag string) (versioning.VersionRecord, error) {
	versions, err := versioning.LoadVersions(featureName)
	if err != nil {
		return versioning.VersionRecord{}, err
	}

	if version > 0 {
		for _, v := range versions {
			if v.Version == version {
				return v, nil
			}
		}
		return versioning.VersionRecord{}, fmt.Errorf("version %d not found", version)
	}

	if tag != "" {
		for _, v := range versions {
			if v.ShortHash == tag || v.FullTag == tag {
				return v, nil
			}
		}
		return versioning.VersionRecord{}, fmt.Errorf("tag/hash %s not found", tag)
	}

	// Default to current version
	currentHash, err := cache.ReadFeatureHash(featureName)
	if err != nil {
		if len(versions) > 0 {
			return versions[len(versions)-1], nil
		}
		return versioning.VersionRecord{}, fmt.Errorf("no versions found and current hash missing")
	}

	for _, v := range versions {
		if v.InputHash == currentHash {
			return v, nil
		}
	}

	if len(versions) > 0 {
		return versions[len(versions)-1], nil
	}
	return versioning.VersionRecord{}, fmt.Errorf("version not found")
}

func RunAction(featureName string, version int, tag string) error {
	v, err := ResolveVersion(featureName, version, tag)
	if err != nil {
		return err
	}

	role := "current"
	if version > 0 || tag != "" {
		role = fmt.Sprintf("v%d", v.Version)
	}

	return docker.RunVersionedContainer(docker.RunOptions{
		Feature:   featureName,
		ImageRef:  v.FullTag,
		Role:      role,
		Version:   v.Version,
		Hash:      v.ShortHash,
		IsManaged: true,
	})
}

// --- main ---
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: builder <command> [args]")
		fmt.Println("Commands: server, build, versions, run")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "server":
		runServer()
	case "build":
		buildCmd := flag.NewFlagSet("build", flag.ExitOnError)
		feature := buildCmd.String("feature", "", "Feature to build")
		buildCmd.Parse(os.Args[2:])
		feat := *feature
		if feat == "" && buildCmd.NArg() > 0 {
			feat = buildCmd.Arg(0)
		}
		if feat == "" {
			fmt.Println("feature is required")
			os.Exit(1)
		}
		if err := BuildFeature(feat); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "versions":
		versionsCmd := flag.NewFlagSet("versions", flag.ExitOnError)
		last := versionsCmd.Int("last", 0, "Show last N versions")
		asJSON := versionsCmd.Bool("json", false, "Output as JSON")
		versionsCmd.Parse(os.Args[2:])
		feature := versionsCmd.Arg(0)
		if feature == "" {
			fmt.Println("feature is required")
			os.Exit(1)
		}
		if err := ListVersions(feature, *last, *asJSON); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "run":
		runCmd := flag.NewFlagSet("run", flag.ExitOnError)
		version := runCmd.Int("version", 0, "Version to run")
		tag := runCmd.String("tag", "", "Tag or hash to run")
		runCmd.Parse(os.Args[2:])
		feature := runCmd.Arg(0)
		if feature == "" {
			fmt.Println("feature is required")
			os.Exit(1)
		}
		if err := RunAction(feature, *version, *tag); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func runServer() {
	r := gin.Default()
	r.POST("/build", func(c *gin.Context) {
		var req struct {
			Feature string `json:"feature"`
		}
		if err := c.BindJSON(&req); err != nil || req.Feature == "" {
			c.JSON(400, gin.H{"error": "feature is required"})
			return
		}

		if err := BuildFeature(req.Feature); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"status": "success", "feature": req.Feature})
	})

	r.Run(":8080")
}
