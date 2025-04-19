/**
 * TableRenderer
 * Handles rendering and updating of the status table
 */
export class TableRenderer {
  constructor() {
    this.statusTable = document.getElementById('statusTable');
    this.gridRenderer = null; // Will be set by the app
    
    // Listen for theme changes to potentially update table styles
    document.addEventListener('themeChanged', () => {
      // Future implementation: update table styles based on theme
    });
  }

  /**
   * Set reference to GridRenderer for height updates
   * @param {GridRenderer} gridRenderer - Reference to grid renderer
   */
  setGridRenderer(gridRenderer) {
    this.gridRenderer = gridRenderer;
  }

  /**
   * Create HTML alert elements based on status data
   * @param {Object} status - Status data for a chain
   * @returns {string} HTML markup for alerts
   * @private
   */
  _createAlerts(status) {
    if (status.active_alerts === 0 && status.last_error === '') {
      return '&nbsp;';
    }
    
    // Add the alert-active class to the container div for pulsing effect
    const alertContainerClass = 'alert-active';

    if (status.last_error !== '') {
      return `
        <div class="${alertContainerClass}">
          <a href="#modal-center-${status.name}" uk-toggle><span class="alert-icon" uk-icon='warning' uk-tooltip="${_.escape(status.active_alerts)} active issues"></span></a>
        </div>
        <div id="modal-center-${_.escape(status.name)}" class="uk-flex-top" uk-modal>
            <div class="uk-modal-dialog uk-modal-body uk-margin-auto-vertical uk-background-secondary">
                <button class="uk-modal-close-default" type="button" uk-close></button>
                <pre class=" uk-background-secondary" style="color: white">${_.escape(status.last_error)}</pre>
            </div>
        </div>
      `;
    } else {
      return `<div class="${alertContainerClass}"><span class="alert-icon" uk-icon='warning' uk-tooltip="${_.escape(status.active_alerts)} active issues"></span></div>`;
    }
  }

  /**
   * Create HTML markup for bonded status
   * @param {Object} status - Status data for a chain
   * @returns {string} HTML markup for bonded status
   * @private
   */
  _createBondedStatus(status) {
    if (status.tombstoned) {
      return "<div class='uk-text-warning'><span uk-icon='ban'></span> <strong>Tombstoned</strong></div>";
    } else if (status.jailed) {
      return "<span uk-icon='warning'></span> <strong>Jailed</strong>";
    } else if (status.bonded) {
      return "<span uk-icon='check'></span>";
    } else {
      return "<span uk-icon='minus-circle'></span> Not active";
    }
  }

  /**
   * Create HTML markup for validator uptime window
   * @param {Object} status - Status data for a chain
   * @returns {string} HTML markup for uptime window
   * @private
   */
  _createUptimeWindow(status) {
    let window = `<div class="uk-width-1-2" style="text-align: end">`;
    
    if (status.missed === 0 && status.window === 0) {
      window += "error</div>";
    } else if (status.missed === 0) {
      window += `100%</div>`;
    } else {
      window += `${(100 - (status.missed / status.window) * 100).toFixed(2)}%</div>`;
    }
    
    window += `<div class="uk-width-1-2">${_.escape(status.missed)} / ${_.escape(status.window)}</div>`;
    
    return window;
  }

  /**
   * Create HTML markup for node status
   * @param {Object} status - Status data for a chain
   * @returns {string} HTML markup for node status
   * @private
   */
  _createNodeStatus(status) {
    let nodes = `${_.escape(status.healthy_nodes)} / ${_.escape(status.nodes)}`;
    
    if (status.healthy_nodes < status.nodes) {
      nodes = "<strong><span uk-icon='arrow-down' style='color: darkorange'></span>" + nodes + "</strong>";
    }
    
    return nodes;
  }

  /**
   * Determine CSS class for height animation
   * @param {string} chainId - Chain ID
   * @param {number} height - Block height
   * @returns {string} CSS animation class
   * @private
   */
  _getHeightAnimationClass(chainId, height) {
    // Check with grid renderer if available
    if (this.gridRenderer && this.gridRenderer.updateBlockHeight(chainId, height)) {
      return 'block-height-change';
    } else {
      return '';
    }
  }

  /**
   * Update the status table with new data
   * @param {Object} status - The status data containing validator information
   */
  updateTable(status) {
    if (!status || !status.Status || !Array.isArray(status.Status)) {
      console.error('Invalid status data for table rendering');
      return;
    }
    
    // Clear the table
    while (this.statusTable.rows.length > 0) {
      this.statusTable.deleteRow(0);
    }
    
    // Render each status row
    for (let i = 0; i < status.Status.length; i++) {
      const chainStatus = status.Status[i];
      const row = this.statusTable.insertRow(i);
      
      // Add class to row if there are alerts or errors
      if (chainStatus.active_alerts > 0 || chainStatus.last_error !== '') {
        row.classList.add('row-has-alert');
      }
      
      // Column 1: Alerts
      row.insertCell(0).innerHTML = `<div>${this._createAlerts(chainStatus)}</div>`;
      
      // Column 2: Chain ID
      row.insertCell(1).innerHTML = `<div>${_.escape(chainStatus.name)} (${_.escape(chainStatus.chain_id)})</div>`;
      
      // Column 3: Height with animation
      const heightClass = this._getHeightAnimationClass(chainStatus.chain_id, chainStatus.height);
      const heightCell = row.insertCell(2);
      heightCell.innerHTML = `<div class="${heightClass}" data-chain="${chainStatus.chain_id}">${_.escape(chainStatus.height)}</div>`;
      heightCell.classList.add('height-data'); // Add class for specific font styling
      
      // Column 4: Moniker
      if (chainStatus.moniker === "not connected") {
        row.insertCell(3).innerHTML = `<div class="uk-text-warning">${_.escape(chainStatus.moniker)}</div>`;
      } else {
        row.insertCell(3).innerHTML = `<div>${_.escape(chainStatus.moniker)}</div>`;
      }
      
      // Column 5: Bonded status
      const bondedStatus = chainStatus.moniker === "not connected" ? "unknown" : this._createBondedStatus(chainStatus);
      row.insertCell(4).innerHTML = `<div style="text-align: center">${bondedStatus}</div>`;
      
      // Column 6: Unvoted Proposals
      row.insertCell(5).innerHTML = `<div style="text-align: center">${chainStatus.unvoted_open_gov_proposals}</div>`;
      
      // Column 7: Uptime window
      const uptimeCell = row.insertCell(6);
      uptimeCell.innerHTML = `<div uk-grid>${this._createUptimeWindow(chainStatus)}</div>`;
      uptimeCell.classList.add('numeric-data'); // Add class for font styling
      
      // Column 8: Threshold
      const thresholdCell = row.insertCell(7);
      thresholdCell.innerHTML = `<div class="uk-text-center"><span class="uk-width-1-2">${100 * chainStatus.min_signed_per_window}%</span></div>`;
      thresholdCell.classList.add('numeric-data'); // Add class for font styling
      
      // Column 9: RPC Nodes
      const rpcCell = row.insertCell(8);
      rpcCell.innerHTML = `<div class="uk-text-center">${this._createNodeStatus(chainStatus)}</div>`;
      rpcCell.classList.add('numeric-data'); // Add class for font styling
    }
  }
} 