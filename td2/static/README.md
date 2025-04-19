# Tenderduty Dashboard

A modern, modular dashboard for monitoring validator nodes in Tendermint-based blockchains.

## Architecture

The application has been refactored to use modern JavaScript ES modules, with a clear separation of concerns:

### Core Modules

- **App** (`app.js`): Main application entry point that initializes and coordinates all other modules
- **DataService** (`data-service.js`): Handles all API communication with the server
- **WebSocketManager** (`websocket-manager.js`): Manages WebSocket connection for real-time updates
- **GridRenderer** (`grid-renderer.js`): Handles canvas drawing for block visualization
- **TableRenderer** (`table-renderer.js`): Manages table updates for validator status
- **LogManager** (`log-manager.js`): Handles log display and management
- **ThemeManager** (`theme-manager.js`): Provides theme switching between light and dark modes
- **Constants** (`constants.js`): Centralizes configuration values and constants

## Features

- Real-time updates of validator status
- Visual block history with color-coded status
- Dark/light theme toggle
- Responsive design with UIKit framework
- Optimized canvas rendering for high-DPI displays
- Automatic reconnection for WebSocket
- Modular codebase for easy maintenance

## Development

### Project Structure

```
├── css/                  # CSS styles 
├── js/                   # JavaScript modules
│   ├── app.js            # Main application
│   ├── constants.js      # Shared constants
│   ├── data-service.js   # API communication
│   ├── grid-renderer.js  # Grid visualization
│   ├── log-manager.js    # Log handling
│   ├── table-renderer.js # Table rendering
│   ├── theme-manager.js  # Theme switching
│   └── websocket-manager.js # Real-time updates
├── favicon.png           # Favicon
├── index.html            # Main HTML page
└── README.md             # Documentation
```

### Adding New Features

To extend the application:

1. Update constants in `constants.js` if needed
2. Modify or create the relevant module
3. Update `app.js` to coordinate new functionality
4. Add required markup to `index.html` if needed

## Credits

Developed by BlockPane for TenderDuty validator monitoring. 