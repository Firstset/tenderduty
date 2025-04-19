/**
 * ThemeManager
 * Handles theme switching between light and dark modes using CSS variables
 */
export class ThemeManager {
  constructor() {
    this.isDark = true;
    
    // Elements that need theme switching
    this.body = document.body;
    // this.logElement = document.getElementById('logs'); // No longer needed, CSS handles log styling
    
    // Initialize theme based on body class
    this.isDark = this.body.classList.contains('uk-light');
  }

  /**
   * Toggle between light and dark themes
   * @returns {Object} Theme configuration with updated status
   */
  toggleTheme() {
    this.isDark = !this.isDark;
    
    if (this.isDark) {
      this._applyDarkTheme();
    } else {
      this._applyLightTheme();
    }
    
    // Dispatch a custom event for other components to react to theme change
    const event = new CustomEvent('themeChanged', {
      detail: { isDark: this.isDark }
    });
    document.dispatchEvent(event);
    
    return { isDark: this.isDark };
  }

  /**
   * Apply dark theme using CSS classes
   * @private
   */
  _applyDarkTheme() {
    // Update body class for global styles
    this.body.classList.remove('uk-text-default');
    this.body.classList.add('uk-background-secondary', 'uk-light');
    
    // No need to update logs element directly, CSS handles it
    // this.logElement.style = "background: #080808; height: 300px;";
  }

  /**
   * Apply light theme using CSS classes
   * @private
   */
  _applyLightTheme() {
    // Update body class for global styles
    this.body.classList.remove('uk-background-secondary', 'uk-light');
    this.body.classList.add('uk-text-default');
    
    // No need to update logs element directly, CSS handles it
    // this.logElement.style = "color: #0a0a0a; background: #dddddd; height: 300px;";
  }

  /**
   * Get current theme configuration
   * @returns {Object} Current theme configuration
   */
  getThemeConfig() {
    return { isDark: this.isDark };
  }
} 