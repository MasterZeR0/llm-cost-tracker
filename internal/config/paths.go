package config

import (
	"os"
	"path/filepath"
	"runtime"
)

func ResolveAppSupportDirs(appName string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var dirs []string
	switch runtime.GOOS {
	case "darwin":
		dirs = append(dirs, filepath.Join(home, "Library", "Application Support", appName))
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			dirs = append(dirs, filepath.Join(appdata, appName))
		}
		dirs = append(dirs, filepath.Join(home, "AppData", "Roaming", appName))
	default: // linux and others
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			dirs = append(dirs, filepath.Join(xdg, appName))
		}
		dirs = append(dirs, filepath.Join(home, ".config", appName))
	}
	return dirs
}

func DefaultPathsForTool(tool string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	switch tool {
	case "cline":
		var paths []string
		for _, codeDir := range ResolveAppSupportDirs("Code") {
			paths = append(paths, filepath.Join(codeDir, "User", "globalStorage", "saoudrizwan.claude-dev", "tasks"))
		}
		return paths
	case "roo_code":
		var paths []string
		for _, codeDir := range ResolveAppSupportDirs("Code") {
			paths = append(paths, filepath.Join(codeDir, "User", "globalStorage", "rooveterinaryinc.roo-cline", "tasks"))
		}
		return paths
	case "cursor":
		var paths []string
		for _, cursorDir := range ResolveAppSupportDirs("Cursor") {
			paths = append(paths, filepath.Join(cursorDir, "User", "workspaceStorage"))
			paths = append(paths, filepath.Join(cursorDir, "User", "globalStorage"))
		}
		return paths
	case "windsurf":
		var paths []string
		for _, windsurfDir := range ResolveAppSupportDirs("Windsurf") {
			paths = append(paths, filepath.Join(windsurfDir, "User", "workspaceStorage"))
		}
		paths = append(paths, filepath.Join(home, ".codeium", "windsurf"))
		return paths
	case "copilot_cli":
		return []string{
			filepath.Join(home, ".copilot-cli"),
			filepath.Join(home, ".config", "gh", "copilot"),
		}
	default:
		return nil
	}
}
