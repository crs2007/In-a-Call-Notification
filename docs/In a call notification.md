Open-source project plan: local call-presence → MQTT
====================================================

This is very feasible, but there is one important architectural constraint that emerged from the research:

**There is no single reliable, vendor-neutral desktop API that tells a local application “the user is currently in a Zoom/Slack/Teams call.”** The best solution is a **multi-signal detector** with app-specific adapters, OS-level signals, and configurable rules. Existing open-source projects already demonstrate several of these techniques individually. ([GitHub](https://github.com/kantselovich/LuxaforPresence?utm_source=chatgpt.com))

I recommend building the project as a **native Go background application with a very small amount of platform-specific code**, rather than Electron or Python.

1\. Research: existing projects and reusable components
=======================================================

1.1 Call / meeting detection
----------------------------

### LuxaforPresence — macOS

**Repository:** kantselovich/LuxaforPresence

[GitHub – LuxaforPresence](https://github.com/kantselovich/LuxaforPresence?utm_source=chatgpt.com)

This is probably the most relevant project conceptually.

It is a native macOS menu-bar application designed specifically to infer whether the user is in a meeting. It combines:

*   Slack Huddle detection through macOS Accessibility
    
*   Teams meeting/call detection through Accessibility
    
*   Zoom process detection
    
*   camera activity
    
*   microphone/voice activity
    
*   optional calendar information
    
*   a manual override
    
*   menu-bar UI
    

The project explicitly treats meeting detection as a heuristic problem rather than assuming a single authoritative signal. It currently reports Slack Huddle and Teams detection as working, while Zoom uses process-based detection. ([GitHub](https://github.com/kantselovich/LuxaforPresence?utm_source=chatgpt.com))

**How we should reuse the idea**

Adopt its fundamental architecture:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML                    `+------------------+                      | Process detector |                      +---------+--------+                                |                      +---------v---------+                      | App-specific      |                      | signal adapters   |                      +---------+---------+                                |        +-----------------------+----------------------+        |                                              |  +-----v------+                                +------v------+  | Mic/camera |                                | Accessibility|  | OS signals |                                | UI signals   |  +-----+------+                                +------+-------+        |                                              |        +-----------------------+----------------------+                                |                         +------v------+                         | Presence /  |                         | call engine |                         +-------------+`

This is much more robust than a single “is Zoom.exe running?” check.

### TeamsBusyLight — Windows

**Repository:** studioab/teamsbusylight

[GitHub – TeamsBusyLight](https://github.com/studioab/teamsbusylight?utm_source=chatgpt.com)

This project is useful for the **Windows implementation**.

Its Teams detector:

*   finds Teams processes
    
*   enumerates all windows belonging to those processes
    
*   examines window titles
    
*   recognizes meeting/call patterns
    
*   explicitly filters out chat and calendar windows
    

For example, it treats titles such as:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Meeting Name | Microsoft | user@company.com | Microsoft Teams   `

as meeting candidates while excluding ordinary chat/calendar windows. ([GitHub](https://github.com/studioab/teamsbusylight?utm_source=chatgpt.com))

It also provides:

*   tray application
    
*   manual override
    
*   status machine
    
*   test mode
    
*   logging
    
*   configurable detection
    
*   startup mechanisms
    

**What to reuse**

The idea of an adapter like:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   type Detector interface {      Name() string      Detect(ctx context.Context) (DetectionResult, error)  }   `

and making Teams one implementation is ideal.

### teams-call — macOS/Linux

**Repository:** mre/teams-call

[GitHub – teams-call](https://github.com/mre/teams-call?utm_source=chatgpt.com)

This project demonstrates another useful technique: monitoring Teams' local log information.

It found local events such as:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   eventData: s::;m::1;a::1   `

for entering a call and:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   eventData: s::;m::1;a::3   `

for leaving a call. ([GitHub](https://github.com/mre/teams-call?utm_source=chatgpt.com))

The project also notes that an older GraphQL-based presence mechanism was deprecated, which is a good warning for our architecture: **avoid depending on undocumented internal web APIs whenever possible.**

**How to use it**

Treat local application logs as an **optional adapter**, not the primary abstraction:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams   ├── Accessibility detector   ├── Window detector   ├── log detector   └── Graph presence adapter [optional]   `

That gives us resilience when Microsoft changes one mechanism.

### TeamsAssist — macOS

**Repository:** TheRudin/TeamsAssist-macOS

[GitHub – TeamsAssist-macOS](https://github.com/TheRudin/TeamsAssist-macOS?utm_source=chatgpt.com)

This project is especially interesting because it demonstrates that Teams detection on macOS can use multiple local signals.

Its README describes:

*   classic Teams log monitoring
    
*   New Teams power assertions
    
*   local status/activity detection
    
*   Home Assistant integration
    
*   no Microsoft Graph admin consent requirement
    

For New Teams, it uses a macOS power assertion associated with an active Teams call. ([GitHub](https://github.com/TheRudin/TeamsAssist-macOS/blob/main/README.md?utm_source=chatgpt.com))

**Use:** excellent source of inspiration for a macOS TeamsDetector.

### BreakTime — Windows

**Repository:** jaimaharaj1/BreakTime

[GitHub – BreakTime](https://github.com/jaimaharaj1/BreakTime?utm_source=chatgpt.com)

This project demonstrates an interesting second class of detector.

It combines:

1.  Windows microphone/webcam activity information
    
2.  process/window detection for Teams, Zoom, Webex and Slack
    
3.  keyword filtering such as Meeting, Call, Sharing
    

It also examines Windows application access state for microphone/webcam. ([GitHub](https://github.com/jaimaharaj1/BreakTime?utm_source=chatgpt.com))

**Use:** excellent reference for the Windows "generic meeting signal" layer.

### EchoPilot — macOS

**Repository:** csmo-it/echopilot

[GitHub – EchoPilot](https://github.com/csmo-it/echopilot?utm_source=chatgpt.com)

Another strong example of multi-signal detection.

Its approach is essentially:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   mic/camera activity         +  application context         +  window title         +  Teams-specific signals         =  meeting likelihood   `

The project explicitly describes its detection as heuristic, with microphone/camera activity providing app-independent evidence and Teams/Zoom/Slack providing contextual evidence. ([GitHub](https://github.com/csmo-it/echopilot?utm_source=chatgpt.com))

This is very close to the architecture I would recommend.

1.2 Microsoft Teams official APIs
=================================

Microsoft Graph does provide presence information, including:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   busy / inACall  busy / inAMeeting   `

and presence change notifications. ([Microsoft Learn](https://learn.microsoft.com/en-us/graph/api/presence-get?view=graph-rest-1.0&utm_source=chatgpt.com))

However, Graph is **not a good primary mechanism for this project**.

Reasons:

*   OAuth/login complexity
    
*   enterprise permissions
    
*   tenant policies
    
*   network dependency
    
*   potentially stale presence
    
*   presence ≠ necessarily exact local call state
    

Microsoft itself notes that aggregated Teams presence can represent multiple presence sessions, and Graph-based presence updates involve polling behavior on the Teams side. ([Microsoft Learn](https://learn.microsoft.com/en-us/graph/cloud-communications-manage-presence-state?utm_source=chatgpt.com))

### Recommendation

Use Graph only as an **optional future adapter**:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams local detection     = default  Teams Graph presence      = optional   `

1.3 Zoom official APIs
======================

Zoom's API exposes presence states such as:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   In_A_Zoom_Meeting  On_A_Call  Presenting  Busy   `

through its user presence API. ([Zoom](https://developers.zoom.us/docs/api/users/?utm_source=chatgpt.com))

Zoom's Meeting SDK also provides detailed information once you're inside a meeting, but that's for applications **using the SDK**, not for externally observing the ordinary Zoom desktop client. ([Zoom](https://developers.zoom.us/docs/meeting-sdk/windows/default-ui/basic-features/in-meeting-user-info/?utm_source=chatgpt.com))

Therefore:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   External desktop detector          ↓  process/window/OS signals   `

is still the appropriate approach.

1.4 Slack
=========

The strongest existing open-source example I found is again LuxaforPresence.

It detects Slack Huddles using macOS Accessibility and recognizes Huddle-specific UI. ([GitHub](https://github.com/kantselovich/LuxaforPresence?utm_source=chatgpt.com))

A useful additional clue is that Slack Huddle windows can expose titles similar to:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Huddle: user - workspace - Slack   `

as shown in community tooling. ([GitHub](https://github.com/hyprwm/Hyprland/discussions/12098?utm_source=chatgpt.com))

For Slack I therefore recommend:

### macOS

Priority:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Accessibility UI       ↓  Huddle window / role / title   `

### Windows

Priority:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Slack process      +  window enumeration      +  title matching   `

with an optional microphone/camera signal to increase confidence.

1.5 Process monitoring
======================

### gopsutil

**Repository:** shirou/gopsutil

[GitHub – gopsutil](https://github.com/shirou/gopsutil?utm_source=chatgpt.com)

gopsutil provides process information on both Windows and macOS, including:

*   PID
    
*   process name
    
*   command line
    
*   executable
    
*   CPU
    
*   threads
    
*   etc.
    

Its current codebase explicitly supports Darwin and Windows process functionality. ([GitHub](https://github.com/shirou/gopsutil?utm_source=chatgpt.com))

Perfect for the generic process layer:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   processes, _ := process.Processes()  for _, p := range processes {      name, _ := p.NameWithContext(ctx)      switch strings.ToLower(name) {      case "zoom":          ...      case "slack":          ...      case "ms-teams":          ...      }  }   `

1.6 Network detection
=====================

Windows Network List Manager
----------------------------

Microsoft provides the native Network List Manager API.

It identifies networks through network signatures and exposes attributes such as connectivity and network category. ([GitHub](https://github.com/MicrosoftDocs/win32/blob/docs/desktop-src/NLA/about-the-network-list-manager-api.md?utm_source=chatgpt.com))

This is preferable to parsing arbitrary command output when implementing a native Windows adapter.

macOS CoreWLAN
--------------

For macOS, the standard native technology is **CoreWLAN**.

Open-source projects such as chbrown/macos-wifi demonstrate accessing:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CWInterface      ↓  SSID  BSSID  RSSI  channel  security   `

([GitHub](https://github.com/chbrown/macos-wifi/blob/master/corewlanlib.swift?utm_source=chatgpt.com))

There is also a newer get-ssid project specifically focused on obtaining the current Wi-Fi SSID on recent macOS versions without relying exclusively on location-gated command-line tools. ([GitHub](https://github.com/fjh658/get-ssid?utm_source=chatgpt.com))

### Important design decision

Do **not** make SSID the only network identity.

Use multiple possible identities:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   SSID  BSSID  gateway IP  local IP  CIDR/subnet  network interface   `

This lets wired Ethernet connections work as well.

1.7 MQTT
========

Eclipse Paho Go
---------------

**Repository:** eclipse-paho/paho.golang

[GitHub – Eclipse Paho Go MQTT v5](https://github.com/eclipse-paho/paho.golang?utm_source=chatgpt.com)

This is my recommended MQTT library.

It supports MQTT 5 and includes autopaho, which handles connection/reconnection. ([GitHub](https://github.com/eclipse-paho/paho.golang?utm_source=chatgpt.com))

There is also the mature MQTT 3.1/3.1.1 Go implementation:

[GitHub – Eclipse Paho MQTT Go](https://github.com/eclipse-paho/paho.mqtt.golang?utm_source=chatgpt.com)

([GitHub](https://github.com/eclipse-paho/paho.mqtt.golang?utm_source=chatgpt.com))

### Recommendation

Use:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   MQTT 5  QoS 1  persistent session where appropriate  automatic reconnect  TLS optional   `

For a local Home Assistant/Mosquitto installation this will work very well.

1.8 System tray
===============

getlantern/systray
------------------

[GitHub – getlantern/systray](https://github.com/getlantern/systray?utm_source=chatgpt.com)

Supports:

*   Windows
    
*   macOS
    
*   Linux
    

and provides menu items, status icons and native tray integration. ([GitHub](https://github.com/getlantern/systray?utm_source=chatgpt.com))

gogpu/systray
-------------

[GitHub – gogpu/systray](https://github.com/gogpu/systray?utm_source=chatgpt.com)

This newer implementation is especially interesting because it supports Windows/macOS/Linux with **zero CGO** and uses platform-native APIs underneath. ([GitHub](https://github.com/gogpu/systray?utm_source=chatgpt.com))

For a modern Go application, this is worth evaluating first.

Python alternative: pystray
---------------------------

[GitHub – pystray](https://github.com/moses-palmer/pystray?utm_source=chatgpt.com)

pystray provides a common tray API and uses native tray backends on Windows/macOS. ([GitHub](https://github.com/moses-palmer/pystray/blob/master/docs/usage.rst?utm_source=chatgpt.com))

I would **not** choose Python as the primary implementation, however.

1.9 Auto-start
==============

### Go-autostart

[GitHub – go-autostart](https://github.com/spiretechnology/go-autostart?utm_source=chatgpt.com)

It supports:

*   Windows
    
*   macOS
    
*   Linux
    

and provides a single API for registering startup applications. ([GitHub](https://github.com/spiretechnology/go-autostart?utm_source=chatgpt.com))

### auto-launch

[GitHub – auto-launch](https://github.com/zzzgydi/auto-launch?utm_source=chatgpt.com)

Another cross-platform startup library supporting Windows and macOS. ([GitHub](https://github.com/zzzgydi/auto-launch?utm_source=chatgpt.com))

2\. Recommended architecture
============================

I would call the project something like:

**CallMQTT**

or:

**MeetingPresence**

or:

**Call2MQTT**

The architecture should look like this:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   ┌─────────────────────────────────────────────────────────────┐  │                        Desktop Agent                         │  │                                                             │  │  ┌───────────────┐    ┌─────────────────┐                  │  │  │ Config Manager│───▶│ Detection Engine│                  │  │  └───────────────┘    └───────┬─────────┘                  │  │                               │                              │  │             ┌─────────────────┼─────────────────┐            │  │             │                 │                 │            │  │       ┌─────▼─────┐     ┌─────▼──────┐    ┌─────▼──────┐    │  │       │ Zoom      │     │ Teams      │    │ Slack      │    │  │       │ Detector  │     │ Detector   │    │ Detector   │    │  │       └─────┬─────┘     └─────┬──────┘    └─────┬──────┘    │  │             │                 │                 │            │  │             └─────────────────┼─────────────────┘            │  │                               │                              │  │                     ┌─────────▼────────┐                     │  │                     │ Presence State   │                     │  │                     │ Machine          │                     │  │                     └─────────┬────────┘                     │  │                               │                              │  │                     ┌─────────▼─────────┐                    │  │                     │ Network Checker   │                    │  │                     └─────────┬─────────┘                    │  │                               │                              │  │                   allowed? ───┴─── yes                       │  │                               │                              │  │                     ┌─────────▼─────────┐                    │  │                     │ MQTT Publisher    │                    │  │                     └─────────┬─────────┘                    │  │                               │                              │  │                        local MQTT broker                     │  │                                                             │  │  ┌──────────────┐      ┌──────────────┐                     │  │  │ Tray Manager │◀────▶│ App State    │                     │  │  └──────────────┘      └──────────────┘                     │  │                                                             │  │  ┌────────────────┐       ┌────────────────┐                 │  │  │ Startup Manager│       │ Logger/Diag    │                 │  │  └────────────────┘       └────────────────┘                 │  └─────────────────────────────────────────────────────────────┘   `

3\. Technology stack
====================

Recommended
-----------

AreaTechnologyCore**Go**Process detectiongopsutilMQTTEclipse Paho GoTraygogpu/systray or getlantern/systrayConfigYAMLLoggingGo slogWindows nativeWin32 via x/sys/windows / small native adaptermacOS nativeCoreWLAN + Accessibility helperAutostartnative platform implementation, optionally go-autostartPackagingGoReleaser + WiX / Homebrew CaskCIGitHub ActionsTestsGo testing + integration fixtures

4\. Why Go over Python/Electron?
================================

Go
--

Advantages:

*   small memory footprint
    
*   single executable
    
*   excellent cross compilation
    
*   no Python installation required
    
*   ideal for a background service
    
*   excellent MQTT support
    
*   excellent process inspection
    
*   good native OS integration
    
*   easier distribution to other users
    

A compiled binary could look like:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   callmqtt.exe   `

or:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT.app   `

with essentially no runtime dependency.

Python
------

Python would be faster for the initial prototype, especially for experimentation.

But you'd eventually have:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Python runtime  PyInstaller  native frameworks  pyobjc  pystray  platform-specific startup logic   `

The packaging experience becomes noticeably more complicated.

Electron
--------

I would avoid Electron completely.

The application doesn't need a browser UI, and Electron would be excessive for:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   process monitor  network monitor  MQTT publisher  tray icon   `

5\. Core data model
===================

Define a standard detection result independent of Zoom/Teams/Slack.

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   type CallState string  const (      CallUnknown CallState = "unknown"      CallInactive CallState = "inactive"      CallActive CallState = "active"  )  type DetectionResult struct {      App         string      State       CallState      Confidence  float64      Reason      string      Timestamp   time.Time  }   `

For example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   {    "app": "teams",    "state": "active",    "confidence": 0.97,    "reason": "meeting-window",    "timestamp": "2026-09-13T11:45:00+03:00"  }   `

6\. Detection engine
====================

The important part is **not** to treat any individual signal as absolute truth.

Use a confidence model.

Example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams process exists                    +0.20  Teams meeting window detected           +0.50  Teams active-call log event              +0.60  Mic active                               +0.15  Camera active                            +0.10  Known meeting accessibility element      +0.40   `

Then clamp:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   confidence = min(1.0, sum(signals))   `

Example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams process       0.20  meeting window      0.50  microphone active   0.15  --------------------------------  confidence          0.85   `

Configuration can define thresholds:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   detection:    active_threshold: 0.70    inactive_threshold: 0.30    enter_debounce_seconds: 2    exit_debounce_seconds: 5   `

This avoids unstable:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   active  inactive  active  inactive   `

transitions.

7\. Detector interface
======================

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   type Detector interface {      App() string      Detect(ctx context.Context) DetectionResult  }   `

Then:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   detectors/      zoom/      teams/      slack/      generic/   `

Platform-specific implementations:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   platform/      windows/          process.go          windows.go          audio.go          network.go          startup.go      darwin/          process.go          accessibility.go          audio.go          network.go          startup.go   `

8\. Zoom detector
=================

Recommended logic:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   1. Is Zoom process running?  2. Enumerate Zoom windows  3. Check title patterns  4. Check known meeting-window characteristics  5. Optionally check microphone/camera use  6. Calculate confidence   `

Conceptually:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   if !zoomRunning {      return inactive()  }  score := 0.20  if meetingWindow {      score += 0.60  }  if microphoneActive {      score += 0.15  }  if cameraActive {      score += 0.10  }  return DetectionResult{      App:        "zoom",      State:      stateFromScore(score),      Confidence: score,  }   `

Do **not** simply use:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   if zoomProcessExists {      return active  }   `

because Zoom can remain open for hours while the user isn't in a meeting.

9\. Microsoft Teams detector
============================

I would implement three adapters.

### Windows

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Process    +  window enumeration    +  window title matching    +  optional microphone state   `

This is directly inspired by TeamsBusyLight. ([GitHub](https://github.com/studioab/teamsbusylight?utm_source=chatgpt.com))

### macOS

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Accessibility       +  Teams logs       +  power assertion       +  process state   `

The existing TeamsAssist and teams-call projects demonstrate several of these techniques. ([GitHub](https://github.com/TheRudin/TeamsAssist-macOS/blob/main/README.md?utm_source=chatgpt.com))

### Optional future

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Microsoft Graph presence   `

using:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   busy + inACall  busy + inAMeeting   `

([Microsoft Learn](https://learn.microsoft.com/en-us/graph/manage-presence-state?utm_source=chatgpt.com))

But keep this disabled by default.

10\. Slack detector
===================

Windows
-------

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Slack process         ↓  window enumeration         ↓  Huddle/Call title patterns         ↓  optional microphone/camera evidence   `

macOS
-----

Use Accessibility to inspect Slack UI.

Apple's Accessibility API exposes UI elements and their state to accessibility applications. ([Apple Developer](https://developer.apple.com/documentation/applicationservices/axuielement_h?utm_source=chatgpt.com))

Potential matching rules:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   slack:    windows:      - title_regex: "(?i)^Huddle:"      - title_regex: "(?i)Slack.*Call"   `

But don't make the exact current Slack title part of the core architecture. Put it in a versioned rule set so it can change without redesigning the application.

11\. Generic microphone/camera detector
=======================================

This should **not** be the main call detector.

It is supporting evidence.

For example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Mic active        +  Zoom process        +  Zoom meeting window        =  very strong evidence   `

versus:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Mic active        +  Spotify running        =  probably not a meeting   `

This concept is also used in projects such as EchoPilot and BreakTime. ([GitHub](https://github.com/csmo-it/echopilot?utm_source=chatgpt.com))

12\. Network checker
====================

This is a critical component because the MQTT action must be gated by the allowed-network rule.

Create:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   type NetworkInfo struct {      Connected     bool      Interface     string      SSID          string      BSSID         string      LocalIP       net.IP      Gateway       net.IP      CIDR          *net.IPNet  }  type NetworkChecker interface {      Current(ctx context.Context) (NetworkInfo, error)  }   `

Then:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML``   type NetworkRule struct {      Name        string   `yaml:"name"`      SSIDs       []string `yaml:"ssids"`      BSSIDs      []string `yaml:"bssids"`      CIDRs       []string `yaml:"cidrs"`      Gateways    []string `yaml:"gateways"`  }   ``

13\. Network matching
=====================

Use OR logic inside a rule:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   SSID matches  OR  BSSID matches  OR  local IP belongs to CIDR  OR  gateway matches   `

Example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   allowed_networks:    - name: Home      ssids:        - "Sharon-Home"      cidrs:        - "192.168.1.0/24"    - name: Office      ssids:        - "Company-WiFi"      cidrs:        - "10.20.0.0/16"    - name: Home-Wired      cidrs:        - "192.168.1.0/24"   `

This is much better than:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   SSID only   `

because Ethernet does not have an SSID.

14\. macOS network implementation
=================================

Prefer native CoreWLAN.

Conceptually:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   let interface = CWWiFiClient.shared().interface()  let ssid = interface?.ssid()  let bssid = interface?.bssid()   `

CoreWLAN exposes the connected Wi-Fi interface and its SSID/BSSID information. ([GitHub](https://github.com/chbrown/macos-wifi/blob/master/corewlanlib.swift?utm_source=chatgpt.com))

However, modern macOS privacy controls can affect SSID access, so the application should degrade gracefully to:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   SSID   ↓ unavailable  BSSID   ↓ unavailable  local IP + gateway + CIDR   `

Projects such as get-ssid specifically address recent macOS SSID-access limitations. ([GitHub](https://github.com/fjh658/get-ssid?utm_source=chatgpt.com))

15\. Windows network implementation
===================================

I'd implement a Windows-native adapter using:

*   Network List Manager
    
*   WLAN APIs when SSID is required
    
*   normal network interface enumeration for IP/CIDR
    

Network List Manager is designed specifically to identify networks and retrieve their attributes. ([Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/netlistmgr/nn-netlistmgr-inetwork?utm_source=chatgpt.com))

16\. MQTT message design
========================

I recommend two MQTT topics.

### State

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   desktop-presence//call   `

### Availability

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   desktop-presence//availability   `

Example payload:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   {    "state": "active",    "app": "teams",    "confidence": 0.94,    "network": "Home",    "timestamp": "2026-09-13T11:45:30+03:00"  }   `

When leaving:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   {    "state": "inactive",    "app": null,    "timestamp": "2026-09-13T12:32:10+03:00"  }   `

17\. Important MQTT behavior when leaving an allowed network
============================================================

There's a subtle problem here.

Suppose:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   11:00 Teams call starts  11:01 connected to allowed Wi-Fi  11:20 laptop switches to mobile hotspot   `

The program must **not publish events once the network is disallowed**, according to your requirement.

That means an MQTT consumer might still believe:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   call = active   `

after the machine disappears from the allowed network.

### Recommended solution

Use MQTT availability/expiry semantics.

For example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   availability = online  state messages have short expiry / heartbeat   `

The subscriber should interpret absence of a fresh heartbeat as:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   unknown/offline   `

This prevents stale state.

18\. Configuration file
=======================

I recommend YAML because users will edit this manually.

Example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   app:    name: CallMQTT    device_id: auto  startup:    enabled: true  tray:    enabled: true  logging:    level: info    file: "~/.callmqtt/callmqtt.log"  polling:    process_seconds: 2    network_seconds: 5  allowed_networks:    - name: Home      ssids:        - "HomeWiFi"      cidrs:        - "192.168.1.0/24"    - name: Office      ssids:        - "Company"      cidrs:        - "10.10.0.0/16"  mqtt:    host: "192.168.1.10"    port: 1883    protocol: "mqtt"    username: "callmqtt"    password: "${CALLMQTT_MQTT_PASSWORD}"    tls:      enabled: false      insecure_skip_verify: false    qos: 1    retain: true  topics:    call: "desktop-presence/{device_id}/call"    availability: "desktop-presence/{device_id}/availability"  detectors:    zoom:      enabled: true    slack:      enabled: true    teams:      enabled: true  detection:    active_threshold: 0.70    inactive_threshold: 0.30    enter_debounce_seconds: 2    exit_debounce_seconds: 5   `

19\. Do not store plain MQTT passwords in YAML
==============================================

The example above is deliberately better than:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   password: "secret123"   `

Preferred options:

### Windows

Use:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Windows Credential Manager / DPAPI   `

### macOS

Use:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Keychain   `

Configuration contains:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   mqtt:    username: "callmqtt"    credential_ref: "mqtt-default"   `

and the application retrieves the secret from the operating system.

For a first MVP, environment variables are acceptable.

20\. Security model
===================

The application should follow a "local-first" philosophy.

### No cloud service required

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Zoom  Teams  Slack     ↓  local agent     ↓  local network validation     ↓  local MQTT broker   `

No meeting data has to leave the computer.

### Do not publish sensitive meeting information

Don't publish:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   meeting title  participant names  conversation text   `

unless explicitly configured.

Prefer:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   {    "app": "teams",    "state": "active"  }   `

rather than:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   {    "meeting": "Project Phoenix - Salary Review",    "participants": [...]  }   `

This makes the tool much more privacy-friendly.

21\. Presence state machine
===========================

This deserves to be an explicit component.

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML                `+-------------+                  |   UNKNOWN   |                  +------+------+                         |                         v                  +-------------+          +------>| INACTIVE    |<------+          |       +------+------+\       |          |              |       \       |          |              | call   \      |          |              | detected\     |          |              v         |      |          |       +-------------+  |      |          +-------| ACTIVE      |--+      |                  +-------------+`

Network state is independent:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   call state       +  network state       ↓  publication decision   `

For example:

CallNetworkPublishinactiveallowedyesactiveallowedyesinactivedisallowednoactivedisallowednounknownallowedprobably no

22\. Event engine pseudocode
============================

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   func evaluate(ctx context.Context) {      detections := detectors.DetectAll(ctx)      call := resolver.Resolve(detections)      network, err := networkChecker.Current(ctx)      if err != nil {          logger.Error("network detection failed", "error", err)          return      }      allowed := networkMatcher.Allowed(network)      stateChanged := stateMachine.Update(call)      if stateChanged && allowed {          mqtt.Publish(callEvent{              State:     call.State,              App:       call.App,              Confidence: call.Confidence,              Network:   network.Name,              Timestamp: time.Now(),          })      }      tray.Update(call, network, allowed)  }   `

23\. Detection resolution
=========================

What happens if:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams = active  Zoom = inactive  Slack = inactive   `

obviously:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams active   `

If:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams = active  Zoom = active   `

do not pick one arbitrarily.

Publish:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   {    "state": "active",    "app": "teams",    "apps": ["teams", "zoom"]  }   `

or define deterministic priority:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams > Zoom > Slack   `

I'd favor the first approach because it accurately represents reality.

24\. Tray application
=====================

The tray icon should expose something like:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT  ────────────────────────  ● Home network: Allowed  ● Call: Microsoft Teams  ● Status: ON CALL  ────────────────────────  ✓ Auto detection  ✓ Publish MQTT  ────────────────────────  Test MQTT  Test Call Detection  Open configuration  Open logs  About  Quit   `

Status should be immediately understandable.

Example icon states:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   gray   = inactive / unknown  green  = monitoring normally  red    = currently on call  yellow = warning/error   `

On macOS, a status bar item can be implemented through native AppKit; on Windows, the standard notification-area APIs work through the tray library. ([GitHub](https://github.com/getlantern/systray?utm_source=chatgpt.com))

25\. Auto-start
===============

Windows
-------

For a tray application:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   HKCU\Software\Microsoft\Windows\CurrentVersion\Run   `

is a simple per-user mechanism.

Windows officially supports startup applications through Run registry keys and Startup folders. ([Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/w8cookbook/startup-apps?utm_source=chatgpt.com))

For enterprise installations, scheduled tasks can also be considered.

### MVP

Use:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   HKCU Run   `

because:

*   no admin rights
    
*   easy enable/disable
    
*   easy to inspect
    
*   perfect for a tray agent
    

macOS
-----

For modern macOS, use:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   SMAppService   `

Apple specifically recommends SMAppService for login items, LaunchAgents and LaunchDaemons on modern macOS. ([Apple Developer](https://developer.apple.com/documentation/servicemanagement/smappservice/mainapp?changes=l_1_1&language=objc&utm_source=chatgpt.com))

For this project:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Login Item / LaunchAgent   `

is appropriate.

It should **not** need to be a root-level daemon.

26\. macOS permissions
======================

This needs to be prominent in the user experience.

The application may need:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Accessibility   `

for Teams/Slack UI inspection.

Apple's Accessibility API exposes accessible application UI elements through AXUIElement. ([Apple Developer](https://developer.apple.com/documentation/applicationservices/axuielement_h?utm_source=chatgpt.com))

Potentially:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Microphone  Camera  Location   `

depending on which detectors are enabled and how network detection is implemented.

The tray menu should therefore show:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Permissions  ────────────────────────────  Accessibility       ✓  Microphone          ✓  Camera              —  Location            ✓  ────────────────────────────  Open Privacy Settings   `

The application should continue running even if a permission is missing.

For example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   No Accessibility        ↓  Slack detection degraded        ↓  process-based detection still works   `

rather than:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   permission denied → application failure   `

27\. Project structure
======================

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   callmqtt/  │  ├── cmd/  │   └── callmqtt/  │       └── main.go  │  ├── internal/  │   ├── config/  │   ├── detection/  │   │   ├── detector.go  │   │   ├── resolver.go  │   │   └── state.go  │   │  │   ├── detectors/  │   │   ├── generic/  │   │   ├── slack/  │   │   ├── teams/  │   │   └── zoom/  │   │  │   ├── network/  │   │   ├── checker.go  │   │   └── matcher.go  │   │  │   ├── mqtt/  │   │   ├── client.go  │   │   └── publisher.go  │   │  │   ├── tray/  │   │   ├── tray.go  │   │   └── icon.go  │   │  │   ├── startup/  │   │   └── startup.go  │   │  │   └── logging/  │  ├── platform/  │   ├── windows/  │   │   ├── windows.go  │   │   ├── windows_audio.go  │   │   ├── windows_network.go  │   │   ├── windows_windows.go  │   │   └── windows_startup.go  │   │  │   └── darwin/  │       ├── darwin.go  │       ├── darwin_accessibility.go  │       ├── darwin_audio.go  │       ├── darwin_network.go  │       └── darwin_startup.go  │  ├── configs/  │   └── example.yaml  │  ├── docs/  │   ├── architecture.md  │   ├── detectors.md  │   ├── windows.md  │   ├── macos.md  │   └── mqtt.md  │  ├── testdata/  │   ├── teams/  │   ├── slack/  │   └── zoom/  │  ├── packaging/  │   ├── windows/  │   └── macos/  │  ├── .github/  │   └── workflows/  │  ├── LICENSE  ├── README.md  ├── CONTRIBUTING.md  ├── SECURITY.md  └── go.mod   `

28\. Development roadmap
========================

Phase 0 — technical spike
-------------------------

Goal: prove the hardest pieces before building UI.

Implement:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Windows:      Teams detection      Zoom detection      Slack detection      network detection  macOS:      Teams detection      Zoom detection      Slack detection      network detection   `

CLI only:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   callmqtt --debug   `

Output:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   [11:45:01] network=Home allowed=true  [11:45:02] teams=inactive  [11:45:02] zoom=active confidence=0.82  [11:45:02] slack=inactive  Resolved:      call=zoom      state=active   `

This phase should happen **before** tray/packaging.

29\. Phase 1 — configuration
============================

Implement:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   config.yaml  validation  defaults  environment substitution   `

Command:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   callmqtt --print-config   `

and:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   callmqtt --validate-config   `

30\. Phase 2 — MQTT
===================

Build:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   connect  authenticate  publish  reconnect  QoS  retained state  availability   `

Test against Mosquitto.

Example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   mosquitto_sub -h 192.168.1.10 \    -t 'desktop-presence/#' \    -v   `

31\. Phase 3 — state machine
============================

Add:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   debounce  confidence  multi-app resolution  network gating  event deduplication   `

Important:

**Only publish when the effective state changes.**

Don't publish:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   ACTIVE  ACTIVE  ACTIVE  ACTIVE   `

every two seconds.

Instead:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   INACTIVE → ACTIVE  ACTIVE → INACTIVE   `

plus periodic availability heartbeat.

32\. Phase 4 — tray
===================

Add:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   status icon  menu  configuration shortcut  test MQTT  test detector  pause detection  quit   `

The detector and MQTT engine should be completely independent of the tray.

That makes headless mode possible later.

33\. Phase 5 — auto-start
=========================

Windows:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   enable()  disable()  isEnabled()   `

macOS:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   enable()  disable()  isEnabled()   `

The UI should show:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Start automatically: ✓   `

34\. Phase 6 — diagnostics
==========================

Add:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   callmqtt diagnose   `

output:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT diagnostics  ─────────────────────────  OS: macOS 15.x  Architecture: arm64  Zoom:    Process: detected    Detector: enabled    Status: inactive  Teams:    Process: detected    Accessibility: granted    Detector: active  Slack:    Process: detected    Accessibility: granted  Network:    Interface: en0    SSID: HomeWiFi    IP: 192.168.1.23    Gateway: 192.168.1.1    Allowed: YES  MQTT:    Broker: 192.168.1.10:1883    Connected: YES   `

This will drastically reduce support problems.

35\. Testing strategy
=====================

Unit tests
----------

Test:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CIDR matching  SSID matching  BSSID matching  network rule evaluation  confidence calculation  state transitions  debouncing  MQTT payload generation  configuration parsing   `

Example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   func TestAllowedCIDR(t *testing.T) {      ...  }   `

Detector tests
--------------

Do not require the actual applications for most tests.

Store samples:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   testdata/      teams/          meeting-window.txt          chat-window.txt      slack/          huddle.txt          normal-window.txt      zoom/          meeting.txt          home.txt   `

Then run:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   input → detector → expected result   `

36\. Integration tests
======================

Use a local Mosquitto broker:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT     ↓  Mosquitto Docker container     ↓  test subscriber   `

CI:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   services:    mosquitto:      image: eclipse-mosquitto   `

Then verify:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   call detected  +  allowed network  =  MQTT event   `

and:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   call detected  +  disallowed network  =  no MQTT event   `

37\. Manual tests
=================

You need an actual test matrix.

### Windows

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Windows 10  Windows 11  Teams Classic if still encountered  New Teams  Zoom  Slack  Wi-Fi  Ethernet  VPN  multiple monitors  screen locked  sleep/wake  startup  logout/login   `

### macOS

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Intel Mac  Apple Silicon  recent macOS  Zoom  Teams  Slack  Wi-Fi  Ethernet  Location disabled  Accessibility disabled  sleep/wake  startup  Fast User Switching   `

The most important manual test is:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   join call  leave call  rejoin  switch network  lock screen  unlock screen   `

while watching MQTT.

38\. Packaging
==============

Windows
-------

Produce:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT-x64.msi  CallMQTT-arm64.msi   `

Potential tooling:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   GoReleaser  +  WiX Toolset   `

Optional package manager distribution:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   winget   `

The Windows installation should install:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT.exe  icons  configuration directory  startup registration   `

39\. macOS packaging
====================

Produce:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT-macOS-universal.dmg   `

containing:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT.app   `

Build:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   x86_64  arm64   `

then create a universal binary.

For a polished public release:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Developer ID signing  +  notarization  +  DMG   `

Homebrew Cask can then distribute it.

40\. GitHub Actions matrix
==========================

I'd use:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   strategy:    matrix:      os:        - windows-latest        - macos-latest   `

and build:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   windows/amd64  windows/arm64  darwin/amd64  darwin/arm64   `

Plus lint:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   golangci-lint  go vet  go test   `

41\. Documentation
==================

README.md
---------

The README should immediately answer:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   What is this?  Why would I use it?  Which applications are supported?  What information leaves my PC?  How do I install it?  How do I configure MQTT?  How do I configure allowed networks?  What permissions are required?   `

Include a diagram:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Zoom / Teams / Slack            ↓       CallMQTT Agent            ↓      Allowed network?         ↙       ↘       YES        NO        ↓          X       MQTT        ↓  Home Assistant / automation   `

CONTRIBUTING.md
---------------

Document:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   development environment  branch policy  detector development  test requirements  PR requirements  platform-specific code  release process   `

SECURITY.md
-----------

Document:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   credential storage  supported security versions  reporting vulnerabilities  privacy model  data collection  network behavior   `

Detector development guide
--------------------------

This will be especially useful for future contributors.

For example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Adding a new detector  1. Create detectors/  2. Implement Detector interface  3. Add Windows rules  4. Add macOS rules  5. Add test fixtures  6. Add configuration  7. Add documentation   `

42\. Community strategy
=======================

GitHub:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Issues  Discussions  Pull Requests  Releases  Security Advisories   `

Useful issue templates:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Bug report  False positive  False negative  New application detector  Platform compatibility  Network detection problem  MQTT issue   `

The most important community mechanism is a **detector compatibility matrix**.

Example:

AppWindowsmacOSMethodZoom✅✅process/window + OS signalsTeams✅✅window/accessibility/logsSlack✅✅window/accessibilityGoogle Meetfuturefuturebrowser/OSWebexfuturefutureprocess/UI

This will allow contributors to add detectors without touching the MQTT or network code.

43\. Future enhancement: plugin architecture
============================================

Once the core is stable:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Detector     |     +-- Zoom     +-- Teams     +-- Slack     +-- Webex     +-- Google Meet     +-- Discord     +-- FaceTime   `

Potentially detectors could be configured as plugins.

However, **don't start with Go plugins**. They introduce deployment complexity.

Instead, start with normal Go interfaces and compile detectors into the application.

44\. Future enhancement: browser meetings
=========================================

A future browser detector could identify:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Chrome  Edge  Safari  Firefox   `

and inspect:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Google Meet  Teams Web  Zoom Web  Slack   `

For browser-based detection, the challenge is distinguishing:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   meeting tab open   `

from:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   actually joined meeting   `

so browser activity should again be treated as a confidence signal rather than absolute truth.

45\. Future enhancement: Google Meet
====================================

For example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Chrome/Edge     +  microphone     +  Meet tab     +  page title   `

could produce:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   confidence = 0.8   `

while generic:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Chrome running   `

produces:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   confidence = 0   `

46\. Future enhancement: Home Assistant integration
===================================================

Since MQTT is the output protocol, Home Assistant can consume it naturally.

For example:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   sensor:    - name: "Desktop Call State"   `

The resulting automation could be:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Desktop on Teams call          +  Home network          ↓  turn office light red  announce "Sharon is on a call"  pause Alexa announcements  set office status   `

The beauty of the project is that **the agent itself doesn't need to know anything about Home Assistant**.

MQTT keeps the boundaries clean.

47\. Future enhancement: web dashboard
======================================

A local dashboard could show:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   CallMQTT  ───────────────────────────────  Current state  🔴 Microsoft Teams  Network  🟢 HomeWiFi  MQTT  🟢 Connected  Detectors  Zoom     ✓  Teams    ✓  Slack    ✓  Last event  11:43:12 Teams → ACTIVE   `

But I would make this a **later feature**.

A browser UI is unnecessary for the MVP.

48\. Future enhancement: configurable rules without recompilation
=================================================================

This is particularly valuable.

Instead of hard-coding:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   "Meeting Name | Microsoft | ... | Microsoft Teams"   `

allow:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   teams:    rules:      - type: window_title        regex: "(?i)Microsoft Teams$"        include:          - "Meeting"          - "Call"      - type: process        names:          - "ms-teams"          - "Teams"   `

Then when Teams changes behavior, users can update the rule without waiting for a binary release.

49\. Future enhancement: detector health
========================================

Expose detector state:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   {    "teams": {      "enabled": true,      "healthy": true,      "confidence": 0.92    }  }   `

The tray can display:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Teams       ✓  Slack       ✓  Zoom        ⚠  MQTT        ✓  Network     ✓   `

This will make real-world troubleshooting much easier.

50\. Recommended MVP scope
==========================

I would **not** attempt to implement everything at once.

### MVP v0.1

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   ✅ Windows  ✅ macOS  ✅ Zoom  ✅ Teams  ✅ Slack  ✅ Allowed SSID  ✅ Allowed CIDR  ✅ MQTT 5  ✅ QoS 1  ✅ Tray icon  ✅ Enable/disable startup  ✅ YAML config  ✅ logging   `

Detection should be:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   process/window signals  +  basic OS signals   `

### v0.2

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   macOS Accessibility  Windows audio/camera signals  confidence scoring  debouncing  diagnostics command  credential storage   `

### v0.3

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   advanced Teams detection  advanced Slack detection  network-change events  availability heartbeat  automatic detector health   `

### v1.0

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   signed Windows installer  signed/notarized macOS app  Homebrew Cask  winget  documentation  detector contribution framework  stable MQTT schema   `

51\. The architecture I would actually build
============================================

The final recommendation is:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML                    `CallMQTT                         │               ┌─────────┴─────────┐               │                   │         Detection Core       Network Core               │                   │        ┌──────┼──────┐            │        │      │      │            │      Zoom   Teams   Slack         │        │      │      │            │        └──────┼──────┘            │               │                   │               └─────────┬─────────┘                         │                   State Machine                         │                   Allowed Network?                      /       \                    YES        NO                     │          │                  MQTT       suppress                     │               Local Broker`

And the technology stack:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Go  ├── gopsutil  ├── Eclipse Paho MQTT v5  ├── gogpu/systray  ├── slog  └── native OS adapters      ├── Windows Win32/WLAN/NLA      └── macOS Accessibility/CoreWLAN/AppKit   `

The **most important architectural principle** is to separate:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   "Can I detect a call?"   `

from:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   "Am I allowed to publish it?"   `

and from:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   "How is the event delivered?"   `

So the system remains:

Plain textANTLR4BashCC#CSSCoffeeScriptCMakeDartDjangoDockerEJSErlangGitGoGraphQLGroovyHTMLJavaJavaScriptJSONJSXKotlinLaTeXLessLuaMakefileMarkdownMATLABMarkupObjective-CPerlPHPPowerShell.propertiesProtocol BuffersPythonRRubySass (Sass)Sass (Scss)SchemeSQLShellSwiftSVGTSXTypeScriptWebAssemblyYAMLXML`   Detector → Presence State → Network Policy → MQTT   `

rather than becoming a collection of Zoom/Teams/Slack-specific scripts.

52\. Best open-source projects to use as the starting references
================================================================

In priority order, I would study:

1.  **LuxaforPresence** — best reference for the overall multi-signal meeting-detection concept on macOS. ([GitHub](https://github.com/kantselovich/LuxaforPresence?utm_source=chatgpt.com))
    
2.  **TeamsBusyLight** — excellent Windows Teams window-detection implementation and tray architecture. ([GitHub](https://github.com/studioab/teamsbusylight?utm_source=chatgpt.com))
    
3.  **teams-call** — useful Teams local-log detection technique. ([GitHub](https://github.com/mre/teams-call?utm_source=chatgpt.com))
    
4.  **TeamsAssist-macOS** — useful modern Teams/macOS-specific approaches. ([GitHub](https://github.com/TheRudin/TeamsAssist-macOS/blob/main/README.md?utm_source=chatgpt.com))
    
5.  **BreakTime** — useful generic Windows microphone/camera + process detection concepts. ([GitHub](https://github.com/jaimaharaj1/BreakTime?utm_source=chatgpt.com))
    
6.  **EchoPilot** — useful example of multi-signal meeting inference. ([GitHub](https://github.com/csmo-it/echopilot?utm_source=chatgpt.com))
    
7.  **gopsutil** — process abstraction. ([GitHub](https://github.com/shirou/gopsutil?utm_source=chatgpt.com))
    
8.  **Eclipse Paho Go** — MQTT transport. ([GitHub](https://github.com/eclipse-paho/paho.golang?utm_source=chatgpt.com))
    
9.  **gogpu/systray** — modern cross-platform tray implementation. ([GitHub](https://github.com/gogpu/systray?utm_source=chatgpt.com))
    
10.  **go-autostart** — cross-platform startup support. ([GitHub](https://github.com/spiretechnology/go-autostart?utm_source=chatgpt.com))
    
11.  **get-ssid** — useful modern macOS SSID implementation reference. ([GitHub](https://github.com/fjh658/get-ssid?utm_source=chatgpt.com))
    

Bottom line
-----------

**Go + native platform adapters + multi-signal detectors + network-policy gate + MQTT** is the architecture I'd choose.

The initial implementation should deliberately avoid depending on private Zoom/Teams/Slack APIs. The existing open-source ecosystem shows that process detection, window inspection, Accessibility, local application logs, and OS device-activity signals are currently the practical techniques for this problem. ([GitHub](https://github.com/kantselovich/LuxaforPresence?utm_source=chatgpt.com))

For a v1 project intended to be genuinely reusable by others, I'd make **detector rules, network rules, MQTT behavior, permissions, and privacy all first-class configuration concepts** rather than hard-coding them around your own machine.