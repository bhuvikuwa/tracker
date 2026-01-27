# GEMINI.md

## Project Overview

This is a Go-based desktop application for Windows that tracks user activity and takes screenshots. It is designed to run as a background service and sends the collected data to a web server via an HTTP API. The application is built using Go and relies on several external libraries for its functionality, including `systray` for the system tray icon, `screenshot` for taking screenshots, and `logrus` for logging.

The application tracks the following user activities:
- Application usage (app name and window title)
- Browser usage (URL and page title)
- Mouse clicks and distance
- Keystrokes

It also captures screenshots at a configurable interval.

## Building and Running

### Prerequisites

- Go 1.21+
- A web server with the PHP endpoint (`collect_data.php`) set up.

### Building

To build the application, run the following command from the root directory:

```bash
go build -o tracker.exe cmd/tracker/main.go
```

### Running

The application can be run as a console application or as a Windows service.

**As a console application:**

```bash
./tracker.exe
```

**As a Windows service:**

To install the service, run:

```bash
./tracker.exe -install
```

To uninstall the service, run:

```bash
./tracker.exe -uninstall
```

## Development Conventions

The project follows standard Go conventions. It is organized into several packages under the `internal` directory, each with a specific responsibility:

- `config`: Manages application configuration from a YAML file.
- `service`: Contains the main application logic and manages the tracker service.
- `storage`: Handles data storage by sending it to a web server.
- `models`: Defines the data models for activities and screenshots.
- `activity`: Contains the logic for tracking user activity.
- `capture`: Contains the logic for capturing screenshots.
- `hooks`: Contains the logic for hooking into keyboard and mouse events.
- `messaging`: Contains the logic for inter-process communication.
- `systray`: Manages the system tray icon and menu.
- `dialog`: Contains the logic for showing dialog boxes.

The application uses `logrus` for logging, and the log level can be configured in the `config.yaml` file.
