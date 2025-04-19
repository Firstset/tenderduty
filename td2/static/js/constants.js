/**
 * Application Constants
 * Centralized configuration values
 */

// Block status codes
export const BLOCK_STATUS = {
  MISSED: 0,
  PREVOTE_MISSED: 1,
  PRECOMMIT_MISSED: 2,
  SIGNED: 3,
  PROPOSED: 4,
  EMPTY_PROPOSED: 5,
  NO_DATA: -1
};

// Grid rendering configuration
export const GRID_CONFIG = {
  CELL_HEIGHT: 24,
  CELL_WIDTH: 9,
  TEXT_MAX_WIDTH: 115,
  TEXT_WIDTH: 120
};

// Theme configuration
export const THEME = {
  DARK: {
    TEXT_COLOR: "#b0b0b0",
    SIGN_COLOR_ALPHA: 0.4,
    BODY_CLASS: "uk-background-secondary uk-light",
    CONTAINER_CLASS: "uk-background-secondary",
    LOG_STYLE: "background: #080808; height: 300px;"
  },
  LIGHT: {
    TEXT_COLOR: "#3f3f3f",
    SIGN_COLOR_ALPHA: 0.2,
    BODY_CLASS: "uk-background-default uk-text-default",
    CONTAINER_CLASS: "uk-background-default",
    LOG_STYLE: "color: #0a0a0a; background: #dddddd; height: 300px;"
  }
};

// API Endpoints
export const API = {
  LOGS_ENABLED: 'logsenabled',
  STATE: 'state',
  LOGS: 'logs',
  WEBSOCKET: 'ws'
};

// WebSocket message types
export const WS_MESSAGE_TYPES = {
  LOG: 'log',
  UPDATE: 'update'
};

// Maximum number of log entries to keep
export const MAX_LOG_ENTRIES = 256; 