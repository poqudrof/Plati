#!/usr/bin/env python3
"""Selenium E2E tests for Plati platform.

Tests:
  1. Login page renders correctly
  2. Admin login with password
  3. Dashboard page renders after login
  4. Navigation links work
  5. New instance page renders with template selector
  6. Settings page renders with SSH key manager
  7. Admin page renders with tabs
  8. Logout works
  9. Protected routes redirect to login
  10. Health endpoint works
  11. SSH key CRUD via UI
  12. Secret CRUD via UI
  13. Fullstack instance lifecycle (create → verify → delete)
  14. Login failure
"""

import sys
import time
import unittest
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.chrome.service import Service
from selenium.webdriver.common.by import By
from selenium.webdriver.support.ui import WebDriverWait
from selenium.webdriver.support import expected_conditions as EC

FRONTEND_URL = "http://localhost:5173"
BACKEND_URL = "http://localhost:8080"
ADMIN_PASSWORD = "12345678"


class PlatiE2ETests(unittest.TestCase):
    """End-to-end tests for the Plati platform."""

    @classmethod
    def setUpClass(cls):
        opts = Options()
        opts.add_argument("--headless")
        opts.add_argument("--no-sandbox")
        opts.add_argument("--disable-dev-shm-usage")
        opts.add_argument("--disable-gpu")
        opts.add_argument("--window-size=1280,900")
        cls.driver = webdriver.Chrome(options=opts)
        cls.driver.implicitly_wait(5)
        cls.wait = WebDriverWait(cls.driver, 10)

    @classmethod
    def tearDownClass(cls):
        cls.driver.quit()

    def _login_admin(self):
        """Helper to log in as admin."""
        self.driver.delete_all_cookies()
        self.driver.get(f"{FRONTEND_URL}/login")
        self.wait.until(EC.presence_of_element_located((By.CSS_SELECTOR, "input[type='password']")))
        pw_input = self.driver.find_element(By.CSS_SELECTOR, "input[type='password']")
        pw_input.clear()
        pw_input.send_keys(ADMIN_PASSWORD)
        # Click the admin login button (second button, or the submit button)
        buttons = self.driver.find_elements(By.CSS_SELECTOR, "button[type='submit']")
        if buttons:
            buttons[0].click()
        else:
            self.driver.find_element(By.XPATH, "//button[contains(text(), 'Admin')]").click()
        # Wait for redirect to dashboard
        self.wait.until(EC.url_contains("/dashboard"))

    # ── Test 1: Login page renders ──────────────────────────────────

    def test_01_login_page_renders(self):
        """Login page should display the Plati title and login form."""
        self.driver.get(f"{FRONTEND_URL}/login")
        self.wait.until(EC.presence_of_element_located((By.CSS_SELECTOR, "input[type='password']")))

        # Check title
        heading = self.driver.find_element(By.TAG_NAME, "h1")
        self.assertEqual(heading.text, "Plati")

        # Check Microsoft sign-in button exists
        ms_button = self.driver.find_element(By.XPATH, "//button[contains(text(), 'Microsoft')]")
        self.assertTrue(ms_button.is_displayed())

        # Check admin password input
        pw_input = self.driver.find_element(By.CSS_SELECTOR, "input[type='password']")
        self.assertTrue(pw_input.is_displayed())

        # Check admin login button
        admin_btn = self.driver.find_element(By.CSS_SELECTOR, "button[type='submit']")
        self.assertTrue(admin_btn.is_displayed())

        print("  PASS: Login page renders correctly")

    # ── Test 2: Admin login ─────────────────────────────────────────

    def test_02_admin_login(self):
        """Admin should be able to log in with password."""
        self._login_admin()

        # Should be on dashboard
        self.assertIn("/dashboard", self.driver.current_url)

        # Navbar should show admin email
        self.wait.until(EC.presence_of_element_located((By.TAG_NAME, "nav")))
        nav_text = self.driver.find_element(By.TAG_NAME, "nav").text
        self.assertIn("admin@plati.local", nav_text)

        print("  PASS: Admin login works")

    # ── Test 3: Dashboard renders ───────────────────────────────────

    def test_03_dashboard_renders(self):
        """Dashboard should show 'My Workspaces' heading."""
        self._login_admin()

        heading = self.wait.until(EC.presence_of_element_located((By.TAG_NAME, "h1")))
        self.assertEqual(heading.text, "My Workspaces")

        # Should have a "New Instance" link/button
        new_btn = self.driver.find_element(By.XPATH, "//a[contains(text(), 'New Instance')]")
        self.assertTrue(new_btn.is_displayed())

        # With no instances, should show empty state
        time.sleep(1)
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertTrue(
            "No workspaces yet" in page_text or "Create your first instance" in page_text,
            f"Expected empty state text, got: {page_text[:200]}"
        )

        print("  PASS: Dashboard renders correctly")

    # ── Test 4: Navigation links ────────────────────────────────────

    def test_04_navigation_links(self):
        """Navbar links should navigate to correct pages."""
        self._login_admin()

        # Check navbar links exist
        nav = self.driver.find_element(By.TAG_NAME, "nav")
        links = nav.find_elements(By.TAG_NAME, "a")
        link_texts = [l.text for l in links]

        self.assertIn("Plati", link_texts)
        self.assertIn("Dashboard", link_texts)
        self.assertIn("New Instance", link_texts)
        self.assertIn("Settings", link_texts)
        self.assertIn("Admin", link_texts)  # Admin user should see admin link

        # Navigate to Settings
        settings_link = nav.find_element(By.XPATH, ".//a[contains(text(), 'Settings')]")
        settings_link.click()
        self.wait.until(EC.url_contains("/settings"))
        self.assertIn("/settings", self.driver.current_url)

        # Navigate to Admin
        nav = self.driver.find_element(By.TAG_NAME, "nav")
        admin_link = nav.find_element(By.XPATH, ".//a[contains(text(), 'Admin')]")
        admin_link.click()
        self.wait.until(EC.url_contains("/admin"))
        self.assertIn("/admin", self.driver.current_url)

        # Navigate back to Dashboard
        nav = self.driver.find_element(By.TAG_NAME, "nav")
        dash_link = nav.find_element(By.XPATH, ".//a[contains(text(), 'Dashboard')]")
        dash_link.click()
        self.wait.until(EC.url_contains("/dashboard"))

        print("  PASS: Navigation links work correctly")

    # ── Test 5: New instance page ───────────────────────────────────

    def test_05_new_instance_page(self):
        """New instance page should show form with template selector."""
        self._login_admin()

        self.driver.get(f"{FRONTEND_URL}/instances/new")
        self.wait.until(EC.presence_of_element_located((By.TAG_NAME, "h1")))

        heading = self.driver.find_element(By.TAG_NAME, "h1")
        self.assertEqual(heading.text, "Create New Instance")

        # Name input
        name_input = self.driver.find_element(By.CSS_SELECTOR, "input#name")
        self.assertTrue(name_input.is_displayed())

        # Template cards should be loaded (we imported node-dev and python-dev)
        time.sleep(2)  # Wait for API call
        page_text = self.driver.find_element(By.TAG_NAME, "main").text

        # Check template names
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertIn("Node.js Development", page_text)
        self.assertIn("Python Development", page_text)

        # Create button should exist but be disabled (no name/template selected)
        create_btn = self.driver.find_element(By.XPATH, "//button[contains(text(), 'Create Instance')]")
        self.assertTrue(create_btn.is_displayed())

        print("  PASS: New instance page renders with templates")

    # ── Test 6: Settings page ───────────────────────────────────────

    def test_06_settings_page(self):
        """Settings page should show SSH key manager and secrets."""
        self._login_admin()

        self.driver.get(f"{FRONTEND_URL}/settings")
        self.wait.until(EC.presence_of_element_located((By.TAG_NAME, "h1")))

        heading = self.driver.find_element(By.TAG_NAME, "h1")
        self.assertEqual(heading.text, "Settings")

        # SSH Keys section
        ssh_heading = self.driver.find_element(By.XPATH, "//*[contains(text(), 'SSH Keys')]")
        self.assertTrue(ssh_heading.is_displayed())

        # SSH key input fields
        key_name_input = self.driver.find_element(By.CSS_SELECTOR, "input[placeholder='Key name']")
        self.assertTrue(key_name_input.is_displayed())

        key_textarea = self.driver.find_element(By.CSS_SELECTOR, "textarea[placeholder*='ssh-']")
        self.assertTrue(key_textarea.is_displayed())

        # Secrets section
        secrets_heading = self.driver.find_element(By.XPATH, "//*[contains(text(), 'Environment Secrets')]")
        self.assertTrue(secrets_heading.is_displayed())

        # Secret input fields
        secret_name = self.driver.find_element(By.CSS_SELECTOR, "input[placeholder='SECRET_NAME']")
        self.assertTrue(secret_name.is_displayed())

        print("  PASS: Settings page renders correctly")

    # ── Test 7: Admin page ──────────────────────────────────────────

    def test_07_admin_page(self):
        """Admin page should show templates, servers, users tabs."""
        self._login_admin()

        self.driver.get(f"{FRONTEND_URL}/admin")
        self.wait.until(EC.presence_of_element_located((By.TAG_NAME, "h1")))

        heading = self.driver.find_element(By.TAG_NAME, "h1")
        self.assertEqual(heading.text, "Admin")

        # Tab buttons
        templates_tab = self.driver.find_element(By.XPATH, "//button[contains(text(), 'Templates')]")
        servers_tab = self.driver.find_element(By.XPATH, "//button[contains(text(), 'Servers')]")
        users_tab = self.driver.find_element(By.XPATH, "//button[contains(text(), 'Users')]")
        self.assertTrue(templates_tab.is_displayed())
        self.assertTrue(servers_tab.is_displayed())
        self.assertTrue(users_tab.is_displayed())

        # Templates tab is active by default - should show templates
        time.sleep(1)
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertIn("Node.js Development", page_text)
        self.assertIn("Import Template", page_text)

        # Switch to Servers tab
        servers_tab.click()
        time.sleep(0.5)

        # Switch to Users tab
        users_tab.click()
        time.sleep(1)
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertIn("admin@plati.local", page_text)
        self.assertIn("Email", page_text)

        print("  PASS: Admin page renders with tabs")

    # ── Test 8: Logout ──────────────────────────────────────────────

    def test_08_logout(self):
        """Logout should clear session and redirect to login."""
        self._login_admin()

        # Click logout button
        logout_btn = self.driver.find_element(By.XPATH, "//button[contains(text(), 'Logout')]")
        logout_btn.click()

        # Should redirect to login
        self.wait.until(EC.url_contains("/login"))
        self.assertIn("/login", self.driver.current_url)

        print("  PASS: Logout works correctly")

    # ── Test 9: Protected routes redirect ───────────────────────────

    def test_09_protected_routes_redirect(self):
        """Unauthenticated access to protected routes should redirect to login."""
        # Clear cookies to ensure logged out
        self.driver.delete_all_cookies()

        self.driver.get(f"{FRONTEND_URL}/dashboard")
        self.wait.until(EC.url_contains("/login"))
        self.assertIn("/login", self.driver.current_url)

        self.driver.get(f"{FRONTEND_URL}/settings")
        self.wait.until(EC.url_contains("/login"))
        self.assertIn("/login", self.driver.current_url)

        self.driver.get(f"{FRONTEND_URL}/admin")
        self.wait.until(EC.url_contains("/login"))
        self.assertIn("/login", self.driver.current_url)

        print("  PASS: Protected routes redirect to login")

    # ── Test 10: API health endpoint ────────────────────────────────

    def test_10_health_endpoint(self):
        """Health endpoint should return ok status."""
        import urllib.request
        import json

        req = urllib.request.urlopen(f"{BACKEND_URL}/health")
        data = json.loads(req.read())
        self.assertEqual(data["status"], "ok")
        self.assertEqual(data["database"], "ok")

        print("  PASS: Health endpoint returns ok")

    # ── Test 11: SSH key CRUD via UI ────────────────────────────────

    def test_11_ssh_key_crud(self):
        """Add and remove an SSH key via the Settings page."""
        self._login_admin()
        self.driver.get(f"{FRONTEND_URL}/settings")
        self.wait.until(EC.presence_of_element_located((By.CSS_SELECTOR, "input[placeholder='Key name']")))

        # Add a test SSH key
        name_input = self.driver.find_element(By.CSS_SELECTOR, "input[placeholder='Key name']")
        name_input.send_keys("test-key")

        key_textarea = self.driver.find_element(By.CSS_SELECTOR, "textarea[placeholder*='ssh-']")
        key_textarea.send_keys("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey123456789 test@plati")

        add_btn = self.driver.find_element(By.XPATH, "//button[contains(text(), 'Add Key')]")
        add_btn.click()

        # Wait for key to appear
        time.sleep(1)
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertIn("test-key", page_text)
        self.assertIn("ssh-ed25519", page_text)

        # Remove the key - find the Remove button in the SSH keys section
        ssh_section = self.driver.find_elements(By.CSS_SELECTOR, ".bg-gray-50")
        for section in ssh_section:
            if "test-key" in section.text:
                section.find_element(By.XPATH, ".//button[contains(text(), 'Remove')]").click()
                break

        time.sleep(2)
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertNotIn("test-key", page_text)

        print("  PASS: SSH key CRUD works via UI")

    # ── Test 12: Secret CRUD via UI ─────────────────────────────────

    def test_12_secret_crud(self):
        """Add and remove a secret via the Settings page."""
        self._login_admin()
        self.driver.get(f"{FRONTEND_URL}/settings")
        self.wait.until(EC.presence_of_element_located((By.CSS_SELECTOR, "input[placeholder='SECRET_NAME']")))

        # Add a secret
        name_input = self.driver.find_element(By.CSS_SELECTOR, "input[placeholder='SECRET_NAME']")
        name_input.send_keys("MY_API_TOKEN")

        value_input = self.driver.find_element(By.CSS_SELECTOR, "input[placeholder='Secret value']")
        value_input.send_keys("super-secret-value-123")

        add_btn = self.driver.find_element(By.XPATH, "//button[contains(text(), 'Add Secret')]")
        add_btn.click()

        time.sleep(1)
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertIn("MY_API_TOKEN", page_text)

        # Remove
        remove_btns = self.driver.find_elements(By.XPATH, "//button[contains(text(), 'Remove')]")
        # The last remove button should be for the secret
        for btn in remove_btns:
            parent_text = btn.find_element(By.XPATH, "..").text
            if "MY_API_TOKEN" in parent_text:
                btn.click()
                break
        time.sleep(1)

        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertNotIn("MY_API_TOKEN", page_text)

        print("  PASS: Secret CRUD works via UI")

    # ── Test 13: Fullstack instance lifecycle (create → terminal → delete) ──

    def test_13_instance_lifecycle(self):
        """Create instance via UI, open terminal, run command, then delete."""
        self._login_admin()

        # ── Create instance ──
        self.driver.get(f"{FRONTEND_URL}/instances/new")
        self.wait.until(EC.presence_of_element_located((By.CSS_SELECTOR, "input#name")))

        name_input = self.driver.find_element(By.CSS_SELECTOR, "input#name")
        name_input.clear()
        name_input.send_keys("selenium-test")

        time.sleep(2)
        template_cards = self.driver.find_elements(
            By.CSS_SELECTOR, ".grid button.rounded-lg"
        )
        self.assertGreater(len(template_cards), 0, "No template cards found")
        template_cards[0].click()
        time.sleep(0.5)

        create_btn = self.driver.find_element(
            By.XPATH, "//button[contains(text(), 'Create Instance')]"
        )
        create_btn.click()

        # Wait for creation + redirect (up to 180s for first image download)
        # Also detect error notifications early (in the fixed notification area)
        try:
            self.wait = WebDriverWait(self.driver, 180)
            self.wait.until(lambda d: (
                "/dashboard" in d.current_url or
                len(d.find_elements(By.CSS_SELECTOR, ".fixed.top-4 .bg-red-600")) > 0
            ))
        finally:
            self.wait = WebDriverWait(self.driver, 10)

        # Check if we got an error notification instead of redirect
        errors = self.driver.find_elements(By.CSS_SELECTOR, ".fixed.top-4 .bg-red-600")
        if errors:
            error_text = errors[0].text
            self.fail(f"Instance creation failed with error: {error_text}")

        self.assertIn("/dashboard", self.driver.current_url,
                      "Expected redirect to dashboard after instance creation")

        # ── Verify on dashboard ──
        time.sleep(3)
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertIn("selenium-test", page_text,
                       f"Instance not visible on dashboard: {page_text[:300]}")

        print("  PASS: Instance created and visible on dashboard")

        # ── Navigate to instance detail page ──
        link = self.driver.find_element(
            By.XPATH, "//a[contains(text(), 'selenium-test')]"
        )
        detail_href = link.get_attribute("href")
        print(f"  Navigating to: {detail_href}")

        # Use direct navigation for reliability
        self.driver.get(detail_href)
        time.sleep(5)

        # Debug: capture page state
        current_url = self.driver.current_url
        print(f"  Current URL: {current_url}")
        if "/dashboard" in current_url or "/login" in current_url:
            # Try to get error info
            page_text = self.driver.find_element(By.TAG_NAME, "main").text
            notifications = self.driver.find_elements(By.CSS_SELECTOR, ".fixed.top-4")
            notif_text = notifications[0].text if notifications else "none"
            print(f"  Page text: {page_text[:200]}")
            print(f"  Notifications: {notif_text}")

            # Check console for JS errors
            logs = self.driver.get_log('browser')
            for l in logs:
                if l['level'] == 'SEVERE':
                    print(f"  JS Error: {l['message']}")

            self.fail(f"Instance detail redirected to {current_url} — instance may have been auto-stopped or auth lost")

        # Wait for h1 to appear
        self.wait = WebDriverWait(self.driver, 15)
        self.wait.until(lambda d: len(d.find_elements(By.TAG_NAME, "h1")) > 0)
        self.wait = WebDriverWait(self.driver, 10)
        heading = self.driver.find_element(By.TAG_NAME, "h1")
        self.assertIn("selenium-test", heading.text,
                       f"Expected instance name in heading, got: {heading.text}")

        # Instance may have been auto-stopped by sleep worker, restart if needed
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        if "stopped" in page_text:
            start_btn = self.driver.find_element(
                By.XPATH, "//button[contains(text(), 'Start')]"
            )
            start_btn.click()
            time.sleep(10)
            self.driver.refresh()
            time.sleep(3)
            page_text = self.driver.find_element(By.TAG_NAME, "main").text

        self.assertIn("running", page_text, f"Instance not running: {page_text[:300]}")

        open_term_btn = self.driver.find_element(
            By.XPATH, "//button[contains(text(), 'Open Terminal')]"
        )
        self.assertTrue(open_term_btn.is_displayed())

        # ── Open terminal ──
        open_term_btn.click()
        time.sleep(3)  # Wait for WebSocket connection + shell prompt

        # Wait for "Connected" indicator to appear
        self.wait.until(
            EC.presence_of_element_located((By.XPATH, "//span[contains(text(), 'Connected')]"))
        )

        # Find the xterm terminal container
        self.wait.until(
            EC.presence_of_element_located((By.CSS_SELECTOR, ".xterm-helper-textarea"))
        )

        print("  PASS: Terminal opened successfully")

        # ── Type command using ActionChains (xterm textarea is hidden) ──
        from selenium.webdriver.common.action_chains import ActionChains

        # Click on the terminal area first to focus it
        term_container = self.driver.find_element(By.CSS_SELECTOR, ".xterm-screen")
        ActionChains(self.driver).click(term_container).perform()
        time.sleep(1)

        # Send keys via ActionChains (types into the focused terminal)
        ActionChains(self.driver).send_keys(
            "cat /root/.ssh/authorized_keys 2>/dev/null || echo 'NO_SSH_KEY'\n"
        ).perform()
        time.sleep(3)

        # Read terminal output from the xterm DOM
        term_rows = self.driver.find_elements(By.CSS_SELECTOR, ".xterm-rows > div")
        term_output = "\n".join([row.text for row in term_rows if row.text.strip()])
        print(f"  Terminal output:\n{term_output}")

        # The command should have produced output (either a key or NO_SSH_KEY)
        self.assertTrue(
            "ssh-" in term_output or "NO_SSH_KEY" in term_output,
            f"Expected SSH key or NO_SSH_KEY in terminal output: {term_output[:500]}"
        )

        print("  PASS: Terminal command executed, output received")

        # ── Close terminal ──
        close_btn = self.driver.find_element(
            By.XPATH, "//button[contains(text(), 'Close Terminal')]"
        )
        close_btn.click()
        time.sleep(1)

        # ── Delete the instance ──
        delete_btn = self.driver.find_element(
            By.XPATH, "//button[contains(text(), 'Delete')]"
        )
        delete_btn.click()
        time.sleep(0.5)
        try:
            self.driver.switch_to.alert.accept()
        except Exception:
            pass

        # Wait for redirect to dashboard
        try:
            self.wait = WebDriverWait(self.driver, 15)
            self.wait.until(EC.url_contains("/dashboard"))
        finally:
            self.wait = WebDriverWait(self.driver, 10)

        time.sleep(3)
        page_text = self.driver.find_element(By.TAG_NAME, "main").text
        self.assertNotIn("selenium-test", page_text,
                          "Instance still visible after deletion")

        print("  PASS: Instance deleted, full lifecycle complete")

    # ── Test 14: Login failure ──────────────────────────────────────

    def test_14_login_failure(self):
        """Wrong password should show error, not redirect."""
        self.driver.delete_all_cookies()
        self.driver.get(f"{FRONTEND_URL}/login")
        self.wait.until(EC.presence_of_element_located((By.CSS_SELECTOR, "input[type='password']")))

        pw_input = self.driver.find_element(By.CSS_SELECTOR, "input[type='password']")
        pw_input.send_keys("wrongpassword")
        self.driver.find_element(By.CSS_SELECTOR, "button[type='submit']").click()

        # Should stay on login page
        time.sleep(2)
        self.assertIn("/login", self.driver.current_url)

        # Error notification should appear
        notifications = self.driver.find_elements(By.CSS_SELECTOR, ".bg-red-600")
        self.assertGreater(len(notifications), 0, "Error notification should be visible")

        print("  PASS: Login failure handled correctly")


if __name__ == "__main__":
    print("\n=== Plati E2E Selenium Tests ===\n")
    unittest.main(verbosity=2)
