package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
)

// Node represents a file
type Node struct {
	ID       string `json:"id"`
	Language string `json:"language"`
}

// Edge represents a dependency
type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// Graph holds the dependency graph
type Graph struct {
	Commit    string `json:"commit"`
	Timestamp string `json:"timestamp"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
}

// parseFile extracts dependencies for a JS/TS file
func parseFile(path string, baseDir string, nodes chan<- Node, edges chan<- Edge, logger *zap.Logger, wg *sync.WaitGroup) {
	defer wg.Done() // Ensure Done is called for all exit paths

	// Only process JS/TS files
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" || !strings.Contains(".js|.jsx|.ts|.tsx", ext) {
		return
	}

	// Add file as a node
	relPath, err := filepath.Rel(baseDir, path)
	if err != nil {
		logger.Warn("Failed to get relative path", zap.String("path", path), zap.Error(err))
		return
	}

	// Send node
	nodes <- Node{ID: relPath, Language: ext[1:]} // e.g., "js" or "ts"

	// Read file
	content, err := os.ReadFile(path)
	if err != nil {
		logger.Warn("Failed to read file", zap.String("path", path), zap.Error(err))
		return
	}

	// Extract imports (ES modules) using regex
	importRegex := `(?m)import\s+(?:[^'"]+\s+from\s+)?['"]([^'"]+)['"];?`
	re := regexp.MustCompile(importRegex)
	matches := re.FindAllStringSubmatch(string(content), -1)
	for _, match := range matches {
		if len(match) > 1 {
			target := resolveImport(match[1], path, baseDir, logger)
			if target != "" {
				edges <- Edge{Source: relPath, Target: target}
			}
		}
	}

	// Fallback for CommonJS (require)
	vm := goja.New()
	_, err = vm.RunString(string(content))
	if err != nil {
		logger.Debug("Skipping invalid JS", zap.String("path", path), zap.Error(err))
		return
	}
	if require := vm.Get("require"); require != nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.Contains(line, "require(") {
				start := strings.Index(line, "('") + 2
				end := strings.Index(line[start:], "')")
				if start > 1 && end > 0 {
					module := line[start : start+end]
					target := resolveImport(module, path, baseDir, logger)
					if target != "" {
						edges <- Edge{Source: relPath, Target: target}
					}
				}
			}
		}
	}

	logger.Debug("File processed", zap.String("path", relPath))
}

var importCache = make(map[string]string)
var importCacheMutex sync.RWMutex

// resolveImport converts import paths to file paths
func resolveImport(module, currentFile, baseDir string, logger *zap.Logger) string {
	importCacheMutex.RLock()
	cachedTarget, ok := importCache[module+currentFile]
	importCacheMutex.RUnlock()
	if ok {
		return cachedTarget
	}

	if strings.HasPrefix(module, "./") || strings.HasPrefix(module, "../") {
		target := filepath.Join(filepath.Dir(currentFile), module)
		if _, err := os.Stat(target); os.IsNotExist(err) {
			for _, ext := range []string{".js", ".jsx", ".ts", ".tsx"} {
				if _, err := os.Stat(target + ext); !os.IsNotExist(err) {
					target += ext
					break
				}
			}
		}
		relTarget, err := filepath.Rel(baseDir, target)
		if err != nil {
			logger.Warn("Failed to get relative target", zap.String("target", target), zap.Error(err))
			return ""
		}
		if !strings.HasPrefix(relTarget, "../") {
			importCacheMutex.Lock()
			importCache[module+currentFile] = relTarget
			importCacheMutex.Unlock()
			return relTarget
		}
	}
	return "" // External modules ignored
}

var logLevel zap.AtomicLevel

func init() {
	logLevel = zap.NewAtomicLevel()
}

func main() {
	repoPath := flag.String("repo", ".", "Path to codebase repository")
	outputDir := flag.String("output", "data", "Output directory for JSON files")
	level := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	flag.Parse()

	// Set log level
	if err := logLevel.UnmarshalText([]byte(*level)); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid log level: %v\n", err)
		os.Exit(1)
	}

	// Configure logger
	config := zap.NewProductionConfig()
	config.Level = logLevel
	logger, err := config.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting program", zap.String("repo", *repoPath), zap.String("output", *outputDir))

	// Open Git repo
	repo, err := git.PlainOpen(*repoPath)
	if err != nil {
		logger.Error("Failed to open repo", zap.Error(err))
		os.Exit(1)
	}

	// Get HEAD
	ref, err := repo.Head()
	if err != nil {
		logger.Error("Failed to get HEAD", zap.Error(err))
		os.Exit(1)
	}

	// Create output dir
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		logger.Error("Failed to create output dir", zap.Error(err))
		os.Exit(1)
	}

	// Get commits
	commitIter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		logger.Error("Failed to get commits", zap.Error(err))
		os.Exit(1)
	}

	// Process commits
	commitCount := 0
	baseDir, _ := filepath.Abs(*repoPath)
	err = commitIter.ForEach(func(c *object.Commit) error {
		commitCount++
		logger.Debug("Processing commit", zap.String("hash", c.Hash.String()))

		// Skip non-merge commits
		if len(c.ParentHashes) <= 1 {
			logger.Debug("Skipping non-merge commit", zap.String("hash", c.Hash.String()))
			return nil
		}

		// Checkout commit
		worktree, err := repo.Worktree()
		if err != nil {
			logger.Error("Failed to get worktree", zap.Error(err))
			return err
		}
		if err := worktree.Checkout(&git.CheckoutOptions{Hash: c.Hash}); err != nil {
			logger.Error("Failed to checkout commit", zap.Error(err))
			return err
		}

		// Initialize graph
		graph := Graph{
			Commit:    c.Hash.String(),
			Timestamp: c.Author.When.Format(time.RFC3339),
			Nodes:     []Node{},
			Edges:     []Edge{},
		}

		// Channels for concurrency
		nodesChan := make(chan Node, 1000) // Increased buffer
		edgesChan := make(chan Edge, 10000)
		var wg sync.WaitGroup

		// Worker pool
		sem := make(chan struct{}, 10)

		// Collect results in a separate goroutine
		done := make(chan struct{})
		nodeMap := make(map[string]Node)
		edgeMap := make(map[string]Edge)
		go func() {
			for {
				select {
				case node, ok := <-nodesChan:
					if !ok {
						done <- struct{}{}
						return
					}
					nodeMap[node.ID] = node
				case edge, ok := <-edgesChan:
					if !ok {
						continue
					}
					edgeMap[edge.Source+edge.Target] = edge
				}
			}
		}()

		// Walk codebase
		err = filepath.WalkDir(*repoPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				logger.Warn("Error walking directory", zap.String("path", path), zap.Error(err))
				return nil
			}
			if d.IsDir() || strings.Contains(path, "node_modules") {
				return nil
			}

			wg.Add(1)
			sem <- struct{}{} // Acquire worker
			go func(path string) {
				defer func() { <-sem }() // Release worker
				parseFile(path, baseDir, nodesChan, edgesChan, logger, &wg)
			}(path)
			return nil
		})
		if err != nil {
			logger.Warn("WalkDir error", zap.Error(err))
		}

		// Wait for parsing to complete
		wg.Wait()
		close(nodesChan)
		close(edgesChan)
		<-done // Wait for collector to finish

		// Populate graph
		for _, node := range nodeMap {
			graph.Nodes = append(graph.Nodes, node)
		}
		for _, edge := range edgeMap {
			if _, exists := nodeMap[edge.Target]; exists {
				graph.Edges = append(graph.Edges, edge)
			}
		}

		logger.Info("Graph generated",
			zap.String("commit", c.Hash.String()),
			zap.Int("nodes", len(graph.Nodes)),
			zap.Int("edges", len(graph.Edges)))

		// Save graph
		outputFile := filepath.Join(*outputDir, c.Hash.String()+".json.gz")
		f, err := os.Create(outputFile)
		if err != nil {
			logger.Error("Failed to create file", zap.String("file", outputFile), zap.Error(err))
			return nil
		}
		defer f.Close()

		gw := gzip.NewWriter(f)
		defer gw.Close()

		if err := json.NewEncoder(gw).Encode(graph); err != nil {
			logger.Error("Failed to encode JSON", zap.String("file", outputFile), zap.Error(err))
			return nil
		}

		logger.Info("Saved graph", zap.String("file", outputFile))
		generateCommitList(*outputDir, c.Hash.String())
		return nil
	})
	if err != nil {
		logger.Error("Error processing commits", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("Processed commits", zap.Int("total", commitCount))
}

// generateCommitList updates commitList.json
var commitListMutex sync.Mutex

func generateCommitList(outputDir, commitHash string) {
	commitListMutex.Lock()
	defer commitListMutex.Unlock()

	jsonFilePath := filepath.Join("web/public", "commitList.json")
	var commitList []string

	if jsonFile, err := os.Open(jsonFilePath); err == nil {
		defer jsonFile.Close()
		if err := json.NewDecoder(jsonFile).Decode(&commitList); err != nil {
			fmt.Println("Error decoding commitList.json:", err)
			return
		}
	} else if !os.IsNotExist(err) {
		fmt.Println("Error opening commitList.json:", err)
		return
	}

	// Append new commit if unique
	for _, c := range commitList {
		if c == commitHash {
			return
		}
	}
	if commitHash != "" {
		commitList = append(commitList, commitHash)
	}

	// Write back
	jsonFile, err := os.Create(jsonFilePath)
	if err != nil {
		fmt.Println("Error creating commitList.json:", err)
		return
	}
	defer jsonFile.Close()

	if err := json.NewEncoder(jsonFile).Encode(commitList); err != nil {
		fmt.Println("Error encoding commitList.json:", err)
		return
	}

	logger, _ := zap.NewProduction()
	logger.Info("Updated commitList.json", zap.String("commit", commitHash))
}
