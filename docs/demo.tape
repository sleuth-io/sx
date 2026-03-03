Output docs/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 600
Set Padding 20

Sleep 500ms

# Run sx init
Type "sx init"
Sleep 300ms
Enter

# "How will you use sx?" - navigate to "Share with my team"
Sleep 1500ms
Down
Sleep 2s
Enter

# "Choose how to share" - navigate to "Git repository"
Sleep 2s
Down
Sleep 1500ms
Enter

# "Enter Git repository URL" - type local vault path
Sleep 500ms
Type "/tmp/team-vault.git"
Sleep 300ms
Enter

# Bootstrap: "Install session hooks?" default [Yes] - accept
Sleep 1s
Enter

# Bootstrap: "Install analytics hooks?" default [Yes] - accept
Sleep 300ms
Enter

# Bootstrap: "Install Sleuth AI Query MCP server?" default [No] - accept
Sleep 300ms
Enter

# Setup complete, "Browse popular community skills?" default [No] - say yes
Sleep 1500ms
Type "y"

# Featured skills menu - navigate to test-driven-development
Sleep 2s
Down
Sleep 1s
Enter

# Download skill from registry, "Detected asset" confirmation - accept
Sleep 2s
Enter

# Version input defaults to "1" - accept
Sleep 1s
Enter

# "Successfully added" - back at skills menu, select "Done"
Sleep 2s
Enter

# "Run install now?" default [Yes] - accept
Sleep 500ms
Enter

# Wait for install to complete
Sleep 4s

# Final output visible
Sleep 2s
