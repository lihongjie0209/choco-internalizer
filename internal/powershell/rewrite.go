package powershell

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_powershell "github.com/wharflab/tree-sitter-powershell/bindings/go"
)

var (
	staticURLAssignment = regexp.MustCompile(`(?is)^\s*\$(url64bit|url64|url32|url)\s*=\s*['"](https?://[^'"\r\n]+)['"]\s*$`)
	staticURLHashEntry  = regexp.MustCompile(`(?is)^\s*(url64bit|url64|url32|url)\s*=\s*['"](https?://[^'"\r\n]+)['"]\s*$`)
	httpReference       = regexp.MustCompile(`(?i)https?://[^\s'"<>]+`)
)

type Resource struct {
	URL      string
	Filename string
	Variable string
}

type edit struct {
	start       uint
	end         uint
	replacement string
}

func Rewrite(script string) (string, []Resource, error) {
	source := []byte(script)
	parser := tree_sitter.NewParser()
	defer parser.Close()
	language := tree_sitter.NewLanguage(tree_sitter_powershell.Language())
	if err := parser.SetLanguage(language); err != nil {
		return "", nil, fmt.Errorf("load PowerShell grammar: %w", err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return "", nil, fmt.Errorf("parse PowerShell: parser returned no tree")
	}
	defer tree.Close()
	root := tree.RootNode()
	if root.HasError() {
		return "", nil, fmt.Errorf("parse PowerShell: syntax tree contains errors")
	}

	var edits []edit
	var resources []Resource
	activeURLs := externalURLs(root, source)
	supportedHelper := false
	zipHelper := false
	walk(root, func(node *tree_sitter.Node) {
		if node.Kind() != "command_name" {
			return
		}
		switch {
		case strings.EqualFold(strings.TrimSpace(node.Utf8Text(source)), "Install-ChocolateyPackage"):
			supportedHelper = true
		case strings.EqualFold(strings.TrimSpace(node.Utf8Text(source)), "Install-ChocolateyZipPackage"):
			supportedHelper, zipHelper = true, true
		}
	})
	hasToolsDir := false
	walk(root, func(node *tree_sitter.Node) {
		nodeText := node.Utf8Text(source)
		switch node.Kind() {
		case "assignment_expression":
			match := staticURLAssignment.FindStringSubmatch(nodeText)
			if match == nil {
				if strings.Contains(strings.ToLower(nodeText), "$toolsdir") {
					hasToolsDir = true
				}
				return
			}
			variable := strings.ToLower(match[1])
			resourceURL := match[2]
			filename := resourceFilename(resourceURL, variable)
			localVariable := "file"
			if variable == "url64bit" {
				localVariable = "file64"
			}
			resources = append(resources, Resource{URL: resourceURL, Filename: filename, Variable: variable})
			edits = append(edits, edit{node.StartByte(), node.EndByte(), fmt.Sprintf("$%s = Join-Path $toolsDir '%s'", localVariable, escapeSingleQuote(filename))})
		case "hash_entry":
			match := staticURLHashEntry.FindStringSubmatch(nodeText)
			if match == nil {
				return
			}
			variable, resourceURL := strings.ToLower(match[1]), match[2]
			filename := resourceFilename(resourceURL, variable)
			localKey := "File"
			if strings.Contains(variable, "64") {
				localKey = "File64"
			}
			if zipHelper {
				localKey = "FileFullPath"
				if strings.Contains(variable, "64") {
					localKey = "FileFullPath64"
				}
			}
			resources = append(resources, Resource{URL: resourceURL, Filename: filename, Variable: variable})
			edits = append(edits, edit{node.StartByte(), node.EndByte(), fmt.Sprintf("%s = Join-Path $toolsDir '%s'", localKey, escapeSingleQuote(filename))})
		case "command_name":
			if strings.EqualFold(strings.TrimSpace(nodeText), "Install-ChocolateyPackage") {
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "Install-ChocolateyInstallPackage"})
			}
			if strings.EqualFold(strings.TrimSpace(nodeText), "Install-ChocolateyZipPackage") {
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "Get-ChocolateyUnzip"})
			}
		case "command_parameter":
			switch {
			case strings.EqualFold(nodeText, "-Url64bit"):
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "-File64"})
			case strings.EqualFold(nodeText, "-Url"):
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "-File"})
			case zipHelper && strings.EqualFold(nodeText, "-UnzipLocation"):
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "-Destination"})
			}
		case "variable":
			if insideStaticURLAssignment(node, source) {
				return
			}
			switch {
			case strings.EqualFold(nodeText, "$url64bit"):
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "$file64"})
			case strings.EqualFold(nodeText, "$url"):
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "$file"})
			case strings.EqualFold(nodeText, "$url64"):
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "$file64"})
			case strings.EqualFold(nodeText, "$url32"):
				edits = append(edits, edit{node.StartByte(), node.EndByte(), "$file"})
			}
		}
	})

	if len(resources) == 0 {
		if len(activeURLs) > 0 {
			return "", nil, fmt.Errorf("no supported static URL assignments found")
		}
		return script, nil, nil
	}
	if !supportedHelper {
		return "", nil, fmt.Errorf("unsupported PowerShell download helper")
	}
	rewritten := applyEdits(source, edits)
	if !hasToolsDir {
		rewritten = "$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition\r\n" + rewritten
	}
	remaining, err := parseExternalURLs(rewritten)
	if err != nil {
		return "", nil, err
	}
	if len(remaining) > 0 {
		return "", nil, fmt.Errorf("unresolved external URLs: %s", strings.Join(remaining, ", "))
	}
	return rewritten, resources, nil
}

func parseExternalURLs(script string) ([]string, error) {
	source := []byte(script)
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_powershell.Language())); err != nil {
		return nil, fmt.Errorf("load PowerShell grammar: %w", err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse rewritten PowerShell: parser returned no tree")
	}
	defer tree.Close()
	if tree.RootNode().HasError() {
		return nil, fmt.Errorf("parse rewritten PowerShell: syntax tree contains errors")
	}
	return externalURLs(tree.RootNode(), source), nil
}

func externalURLs(root *tree_sitter.Node, source []byte) []string {
	var result []string
	walk(root, func(node *tree_sitter.Node) {
		if node.Kind() != "string_literal" {
			return
		}
		result = append(result, httpReference.FindAllString(node.Utf8Text(source), -1)...)
	})
	return result
}

func insideStaticURLAssignment(node *tree_sitter.Node, source []byte) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == "assignment_expression" {
			return staticURLAssignment.MatchString(parent.Utf8Text(source))
		}
	}
	return false
}

func walk(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	visit(node)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		walk(node.NamedChild(i), visit)
	}
}

func applyEdits(source []byte, edits []edit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	result := append([]byte(nil), source...)
	lastStart := uint(len(source) + 1)
	for _, current := range edits {
		if current.end > lastStart {
			continue
		}
		result = append(result[:current.start], append([]byte(current.replacement), result[current.end:]...)...)
		lastStart = current.start
	}
	return string(result)
}

func resourceFilename(rawURL, fallback string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fallback + ".bin"
	}
	filename := path.Base(parsed.Path)
	if filename == "." || filename == "/" || filename == "" {
		return fallback + ".bin"
	}
	return filename
}

func escapeSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
