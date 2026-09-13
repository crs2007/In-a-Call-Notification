1\. Project Review
------------------

**Project Scope & Core Problem:**The project, CallMQTT, is a lightweight, native Go background agent designed to infer when a user is in a call (Zoom, Teams, Slack) and publish this state to a local MQTT broker. Because there is no single, reliable desktop API for cross-vendor call presence, the core problem is heuristic detection: the system must aggregate multi-source signals (processes, window titles, OS-level microphone/camera activity) into a unified confidence score. The output is strictly gated by the current network (SSID/CIDR) to prevent publishing presence data when off-site. **Target Platforms:**

*   Windows (10/11).
    
*   macOS (Intel/Apple Silicon).
    

**Risks & Constraints:**

*   **Permissions:** macOS requires explicit Accessibility permissions to read UI elements (e.g., Slack Huddles), and potentially Microphone/Camera/Location access. The app must degrade gracefully (e.g., falling back to process detection) if permissions are denied.
    
*   **Stale State:** If a laptop disconnects from an allowed network or goes to sleep mid-call, the MQTT broker might retain a stale "active" state. This requires utilizing MQTT availability/expiry semantics (heartbeats).
    
*   **State Flapping:** Toggling video or momentarily dropping a call can cause state jitter. Debouncing logic is mandatory.
    
*   **Detection Fragility:** Relying on strict window titles means updates to Teams or Slack could break detection. Rules must be abstracted into configuration, not hardcoded.
    

2\. Detailed Task Plan (Vibe-Coding Optimized)
----------------------------------------------

This plan is structured for rapid iteration using AI coding extensions. Feed these tasks individually to your AI assistant to maintain tight context windows.**⚠️ Over-Engineering Flags (Keep it Simple):**

*   **Do not use official OAuth APIs (Microsoft Graph, Zoom SDK).** They require complex auth flows and cloud dependencies. Rely exclusively on local OS signals.
    
*   **Do not build a web dashboard or use Electron/Python.** Build a single compiled Go binary with a minimal native system tray.
    
*   **Do not build a dynamic Go plugin system.** Compile detectors directly into the binary using standard Go interfaces.
    

**IDPhase & Task Prompt for AIDepends OnParallelizable1Phase 1: Core Foundation**Create a Go module. Define a YAML configuration parser using 'gopkg.in/yaml.v3'. Implement a struct for allowed networks (SSID, CIDR) and MQTT credentials.NoneNo**2**Define the core data model: CallState (active, inactive, unknown) and a DetectionResult struct (App, State, Confidence, Reason). 1Yes**3Phase 2: Network Policy**Implement a NetworkChecker interface. For now, mock it to return a static local IP and SSID. Create a NetworkMatcher that evaluates if the current network matches the allowed YAML config via CIDR or SSID. 1Yes**4Phase 3: State Machine & MQTT**Write a StateMachine struct that accepts DetectionResults. Implement a debouncer (enter\_debounce/exit\_debounce) and a confidence clamping function (sum signals, cap at 1.0). 2No**5**Implement an MQTT publisher using 'eclipse-paho/paho.golang' (MQTT v5). Implement a QoS 1 connection with automatic reconnect. Add a heartbeat payload to a /availability topic, and state changes to a /call topic. 4No**6Phase 4: Generic Detectors**Import 'shirou/gopsutil'. Implement a generic process detector that scans for target executables (Zoom, ms-teams, Slack). Return a baseline confidence score (e.g., 0.20) if found. 2Yes**7Phase 5: Platform Adapters (macOS)**Implement a macOS-specific network adapter using CoreWLAN to fetch SSID/BSSID. Create a graceful fallback to local IP/Gateway if privacy settings block SSID. 3Yes (OS)**8**Implement a macOS accessibility detector using AXUIElement to scrape active window titles for Slack Huddles and Teams calls. 6Yes (OS)**9Phase 5: Platform Adapters (Windows)**Implement a Windows network adapter using Network List Manager and standard interface enumeration for IP/CIDR. 3Yes (OS)**10**\`Implement a Windows window enumerator using Win32 APIs to match known active meeting window titles (e.g., "Meeting NameMicrosoft Teams").\` 6**11Phase 6: Tray & Polish**Import 'gogpu/systray' (or getlantern). Implement a minimal tray icon showing current detection status, MQTT connection state, and a quit button. 5, 6No

3\. Coding Standards (Best Practices)
-------------------------------------

*   **Directory Structure:** Use standard Go layout. cmd/callmqtt/ for the entry point, internal/ for private business logic (config, detection, network, mqtt), and platform/ for OS-specific adapters (windows/, darwin/).
    
*   **Multi-Platform Code:** Keep platform-specific code isolated using Go build tags (e.g., //go:build windows). The core internal/detection logic should only interact with OS code via abstracted Go interfaces (e.g., Detector.Detect(ctx)).
    
*   **Logging & Diagnostics:** Use standard library log/slog for structured logging. Implement a --debug or diagnose CLI flag that dumps OS info, permission states, network interface data, and raw detector outputs to terminal before the tray UI initializes.
    
*   **Configuration Management:** Use YAML. Ensure sensitive data (MQTT passwords) can be injected via environment variables initially. Avoid committing secrets to the config file.
    
*   **Graceful Degradation:** Never crash if an OS API throws a permission error. Log the failure, return a zero-confidence score for that specific signal, and allow other heuristic signals (like process polling) to continue.
    

4\. Testing Strategy (Low-Resource & Time-Constrained)
------------------------------------------------------

*   **Unit Tests (Critical Paths):** Focus strictly on isolated logic that requires no OS APIs.
    
    *   Test IP/CIDR matching logic to ensure strict network gating.
        
    *   Test the confidence math and debouncer state transitions.
        
*   **Integration Tests (Fixture Based):** Do not require real Zoom/Teams installations for testing. Save text dumps of active/inactive window titles and local log outputs into a testdata/ directory. Feed these text fixtures into your detectors to assert correct state resolution.
    
*   **MQTT Integration:** Spin up a lightweight eclipse-mosquitto Docker container. Write a test that simulates a call detection on an allowed network and verifies the correct MQTT payload is published, then simulate a network change and ensure publishing stops.
    
*   **CI Setup:** Use GitHub Actions. Define a simple matrix os: \[windows-latest, macos-latest\] that runs golangci-lint, go test, and compiles binaries for amd64 and arm64.
    
*   **Manual Smoke Test:** The ultimate pass/fail is physical: Join a call -> Verify MQTT output -> Disconnect from Wi-Fi -> Verify MQTT availability drops -> Reconnect -> Leave call -> Verify inactive state.
    

Which specific OS platform (Windows or macOS) do you plan to tackle the low-level API adapters for first?