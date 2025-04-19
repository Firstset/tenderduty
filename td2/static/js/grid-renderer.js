/**
 * GridRenderer
 * Handles visualization for the block grid using modern DOM elements
 */
import { BLOCK_STATUS } from './constants.js';

export class GridRenderer {
  constructor() {
    this.gridContainer = document.getElementById('grid-container');
    
    // Debug: Check if grid container exists
    if (!this.gridContainer) {
      console.error('Grid container not found! Make sure grid-container element exists in HTML');
    }
    
    this.blocksPerRow = 500; // Number of blocks to show for each chain
    this.blockHeights = new Map(); // Track block heights for animation
  }

  /**
   * Draw the block grid visualization
   * @param {Object} data - The state data containing status information
   */
  drawSeries(data) {
    
    if (!data || !data.Status || !data.Status.length) {
      console.error('Invalid state data for grid rendering');
      return;
    }
    
    if (!this.gridContainer) {
      console.error('Cannot render grid: grid-container element not found');
      // Try to get the container again, it might have loaded since initialization
      this.gridContainer = document.getElementById('grid-container');
      if (!this.gridContainer) {
        return;
      }
    }

    // Clear the grid container
    this.gridContainer.innerHTML = '';
    
    // Create a row for each chain
    data.Status.forEach((chainStatus, chainIndex) => {
      // Create row element
      const row = document.createElement('div');
      row.className = 'chain-row';
      
      // Add chain name label
      const label = document.createElement('div');
      label.className = 'chain-label';
      label.textContent = chainStatus.name;
      label.setAttribute('data-tooltip', chainStatus.name);
      row.appendChild(label);
      
      // Create blocks container
      const blocksContainer = document.createElement('div');
      blocksContainer.className = 'blocks-container';
      
      // Check how blocks are stored in the status data
      // The original implementation may be using a different data structure
      let blockData = [];
      
      if (chainStatus.blocks && Array.isArray(chainStatus.blocks)) {
        blockData = chainStatus.blocks;
      } else if (typeof chainStatus.blocks === 'object') {
        // Convert object to array if needed
        blockData = Object.values(chainStatus.blocks);
      } else {
        console.warn(`Chain ${chainStatus.name} has no blocks data or unsupported format`);
      }
      
      // Ensure we have some blocks to show even if data is missing
      if (blockData.length === 0) {
        blockData = new Array(50).fill(BLOCK_STATUS.NO_DATA);
      }
      
      // Add individual blocks
      const blockDataLength = blockData.length;
      blockData.forEach((blockStatus, blockIndex) => {
        // Calculate actual block number assuming array is ordered oldest to newest
        const actualBlockNumber = chainStatus.height 
          ? chainStatus.height - (blockDataLength - 1 - blockIndex) 
          : blockIndex + 1; // Fallback if height is missing
        
        const block = this._createBlockElement(blockStatus, chainIndex, blockIndex, actualBlockNumber);
        blocksContainer.appendChild(block);
      });
      
      row.appendChild(blocksContainer);
      this.gridContainer.appendChild(row);
      
      // Also store the current block height for animation tracking
      if (chainStatus.chain_id && chainStatus.height) {
        this.blockHeights.set(chainStatus.chain_id, chainStatus.height);
      }
    });
    
  }
  
  /**
   * Create a single block element
   * @param {number} status - Block status code
   * @param {number} chainIndex - Index of the chain
   * @param {number} blockIndex - Index of the block within the displayed array
   * @param {number} actualBlockNumber - The actual block height number
   * @returns {HTMLElement} Block element
   * @private
   */
  _createBlockElement(status, chainIndex, blockIndex, actualBlockNumber) {
    const block = document.createElement('div');
    block.className = 'block';
    
    // Convert status to number if it's a string
    const blockStatus = parseInt(status);
    let tooltipText = 'Unknown status';
    
    // Base status text
    let statusText = 'Unknown';
    
    // Apply status-specific class and set tooltip text
    switch (blockStatus) {
      case BLOCK_STATUS.PROPOSED: // 4
        block.classList.add('status-proposed');
        statusText = 'Proposed';
        break;
      case BLOCK_STATUS.EMPTY_PROPOSED: // 5
        block.classList.add('status-empty-proposed');
        statusText = 'Proposed (Empty)';
        break;
      case BLOCK_STATUS.SIGNED: // 3
        block.classList.add('status-signed');
        statusText = 'Signed';
        // Different styling for even/odd rows
        block.classList.add(chainIndex % 2 === 0 ? 'even' : 'odd');
        break;
      case BLOCK_STATUS.PRECOMMIT_MISSED: // 2
        block.classList.add('status-miss-precommit');
        statusText = 'Missed Precommit';
        break;
      case BLOCK_STATUS.PREVOTE_MISSED: // 1
        block.classList.add('status-miss-prevote');
        statusText = 'Missed Prevote';
        break;
      case BLOCK_STATUS.MISSED: // 0
        block.classList.add('status-missed');
        statusText = 'Missed';
        // Add white line for missed blocks
        const line = document.createElement('div');
        line.className = 'block-line';
        block.appendChild(line);
        break;
      default:
        block.classList.add('status-no-data');
        statusText = 'No Data';
    }
    
    // Combine actual block number and status text for tooltip
    tooltipText = `Block ${actualBlockNumber}: ${statusText}`;
    
    // REMOVED: Add tooltip attribute
    // block.setAttribute('data-tooltip', tooltipText);
    // DEBUG: Verify tooltip attribute is set
    //console.log('Set tooltip for block:', blockIndex, ':', block.getAttribute('data-tooltip'));
    
    return block;
  }
  
  /**
   * Update block height and determine if animation should be triggered
   * @param {string} chainId - Chain ID
   * @param {number} height - Block height
   * @returns {boolean} True if height changed and animation should be triggered
   */
  updateBlockHeight(chainId, height) {
    const previousHeight = this.blockHeights.get(chainId);
    const changed = previousHeight !== undefined && previousHeight !== height;
    
    // Update stored height
    this.blockHeights.set(chainId, height);
    
    // If height changed, update visual representation
    if (changed) {
      this.animateHeightChange(chainId, height);
    }
    
    return changed;
  }

  /**
   * Animate height change for a specific chain
   * @param {string} chainId - Chain ID
   * @param {number} newHeight - New block height
   */
  animateHeightChange(chainId, newHeight) {
    // Update height elements in the table (if they exist)
    const heightElements = document.querySelectorAll(`[data-chain="${chainId}"]`);

    heightElements.forEach(element => {
      // Update text
      element.textContent = newHeight;
      
      // Apply animation
      element.classList.remove('block-height-change');
      void element.offsetWidth; // Force reflow to restart animation
      element.classList.add('block-height-change');
    });
  }
} 