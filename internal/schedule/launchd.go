package schedule

import (
	"encoding/xml"
	"fmt"
	"strings"
	"text/template"

	"github.com/antonL95/promptstor/internal/paths"
)

const launchdLabel = "com.promptstor.check"

var plistTemplate = template.Must(template.New("plist").Funcs(template.FuncMap{"xml": escapeXML}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{xml .Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{xml .Binary}}</string>
		<string>doctor</string>
		<string>--check</string>
		<string>--notify</string>
	</array>
{{- if .Variables}}
	<key>EnvironmentVariables</key>
	<dict>
{{- range .Variables}}
		<key>{{xml .Name}}</key>
		<string>{{xml .Value}}</string>
{{- end}}
	</dict>
{{- end}}
	<key>StartInterval</key>
	<integer>3600</integer>
	<key>RunAtLoad</key>
	<true/>
	<key>StandardOutPath</key>
	<string>{{xml .Log}}</string>
	<key>StandardErrorPath</key>
	<string>{{xml .Log}}</string>
</dict>
</plist>
`))

type launchd struct{ Scheduler }

func (l launchd) install() (string, error) {
	path := l.Env.LaunchAgentPath(launchdLabel)
	if err := l.prepareLog(); err != nil {
		return "", err
	}
	data := struct {
		Label, Binary, Log string
		Variables          []paths.Variable
	}{launchdLabel, l.Binary, l.Env.CheckLogPath(), l.Env.LocationOverrides()}
	if err := l.writeDefinition(path, plistTemplate, data); err != nil {
		return "", err
	}
	_, _ = l.run("launchctl", "bootout", l.service())
	if _, err := l.run("launchctl", "bootstrap", l.domain(), path); err != nil {
		return "", err
	}
	return fmt.Sprintf("Scheduled an hourly check with launchd: %s", path), nil
}

func (l launchd) remove() error {
	if _, err := l.run("launchctl", "bootout", l.service()); err != nil && l.loaded() {
		return err
	}
	return l.removeDefinition(l.Env.LaunchAgentPath(launchdLabel))
}

func (l launchd) status() (string, error) {
	path := l.Env.LaunchAgentPath(launchdLabel)
	found, err := exists(path)
	if err != nil {
		return "", err
	}
	if !found {
		return notInstalled, nil
	}
	state := "installed but not active"
	if l.loaded() {
		state = "installed and active"
	}
	return fmt.Sprintf("launchd agent %s: %s", state, path), nil
}

func (l launchd) loaded() bool {
	_, err := l.run("launchctl", "print", l.service())
	return err == nil
}

func (l launchd) domain() string { return fmt.Sprintf("gui/%d", l.Getuid()) }

func (l launchd) service() string { return l.domain() + "/" + launchdLabel }

func escapeXML(s string) (string, error) {
	var escaped strings.Builder
	if err := xml.EscapeText(&escaped, []byte(s)); err != nil {
		return "", err
	}
	return escaped.String(), nil
}
