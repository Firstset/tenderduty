/**
 * LogManager
 * Handles log entries display and management
 */
import { MAX_LOG_ENTRIES } from './constants.js';

export class LogManager {
  constructor(maxLogEntries = MAX_LOG_ENTRIES) {
    this.logs = new Array(1);
    this.maxLogEntries = maxLogEntries;
    this.logsElement = document.getElementById('logs');
  }

  /**
   * Format timestamp to locale time string
   * @param {number} timestamp - Unix timestamp in seconds
   * @returns {string} Formatted timestamp
   * @private
   */
  _formatTimestamp(timestamp) {
    return new Date(timestamp * 1000).toLocaleTimeString();
  }

  /**
   * Add a log message to the log display
   * @param {number} timestamp - Unix timestamp in seconds
   * @param {string} message - Log message content
   */
  addLogMessage(timestamp, message) {
    // Create formatted message
    let formattedMessage = '';
    
    if (timestamp === 0) {
      formattedMessage = '';
    } else {
      formattedMessage = `${this._formatTimestamp(timestamp)} - ${message}`;
    }
    
    // Maintain maximum log size
    if (this.logs.length >= this.maxLogEntries) {
      this.logs.pop();
    }
    
    // Add new message to beginning of array
    this.logs.unshift(formattedMessage);
    
    // Update display if page is visible
    this._updateLogDisplay();
  }

  /**
   * Load initial log entries
   * @param {Array} logEntries - Array of log entry objects
   */
  loadInitialLogs(logEntries) {
    if (!Array.isArray(logEntries)) return;
    
    // Process logs in reverse order (newest first)
    for (let i = logEntries.length - 1; i >= 0; i--) {
      const entry = logEntries[i];
      
      if (entry.ts === 0) {
        this.addLogMessage(0, '');
        continue;
      }
      
      this.addLogMessage(entry.ts, entry.msg);
    }
  }

  /**
   * Update the log display element with current logs
   * @private
   */
  _updateLogDisplay() {
    // Only update if document is visible
    if (document.visibilityState !== 'hidden' && this.logsElement) {
      this.logsElement.textContent = this.logs.join('\n');
    }
  }

  /**
   * Clear all log entries
   */
  clearLogs() {
    this.logs = new Array(1);
    this._updateLogDisplay();
  }
} 