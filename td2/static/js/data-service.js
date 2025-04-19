/**
 * DataService
 * Handles all API requests to the server
 */
import { API } from './constants.js';

export class DataService {
  constructor() {
    this.baseUrl = window.location.protocol === 'https:' ? 
      window.location.origin : 
      'http://127.0.0.1:8888';
    
    this.fetchOptions = {
      method: 'GET',
      mode: 'cors',
      cache: 'no-cache',
      credentials: 'same-origin',
      redirect: 'error',
      referrerPolicy: 'no-referrer'
    };
  }

  /**
   * Get API URL with endpoint
   * @param {string} endpoint - API endpoint
   * @returns {string} Full API URL
   */
  _getUrl(endpoint) {
    return `${this.baseUrl}/${endpoint}`;
  }

  /**
   * Fetch data from API and parse as JSON
   * @param {string} endpoint - API endpoint
   * @returns {Promise<Object>} Parsed JSON response
   */
  async _fetchData(endpoint) {
    try {
      const response = await fetch(this._getUrl(endpoint), this.fetchOptions);
      return await response.json();
    } catch (error) {
      console.error(`Error fetching ${endpoint}:`, error);
      return null;
    }
  }

  /**
   * Check if logs are enabled
   * @returns {Promise<Object>} Log enabled status
   */
  async checkLogsEnabled() {
    return await this._fetchData(API.LOGS_ENABLED);
  }

  /**
   * Fetch application state
   * @returns {Promise<Object>} Application state data
   */
  async fetchState() {
    return await this._fetchData(API.STATE);
  }

  /**
   * Fetch logs
   * @returns {Promise<Array>} Log entries
   */
  async fetchLogs() {
    return await this._fetchData(API.LOGS);
  }

  /**
   * Load initial state and logs
   * @returns {Promise<Object>} Combined state data
   */
  async loadState() {
    try {
      // Check if logs are enabled
      const logsStatus = await this.checkLogsEnabled();
      if (logsStatus && logsStatus.enabled === false) {
        document.getElementById("logContainer").hidden = true;
      }

      // Load state data
      const state = await this.fetchState();
      
      // Load logs if container is visible
      if (!document.getElementById("logContainer").hidden) {
        const logs = await this.fetchLogs();
        return { ...state, logs };
      }
      
      return state;
    } catch (error) {
      console.error('Error loading initial state:', error);
      return null;
    }
  }
} 