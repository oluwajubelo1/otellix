# VHS tape for demonstrating Otellix real-time observability
# Install with: brew install vhs
# Run with: vhs < demo.tape

Output assets/demo.gif

Set FontSize 22
Set Width 1200
Set Height 600
Set Padding 40
Set WindowBar Fixed
Set TypingSpeed 100ms

# Start the session
Type "go run examples/realtime-stream/main.go"
Sleep 500ms
Enter

# Wait for prompt
Sleep 2s
Type "Explain why Otellix is a game-changer for Gophers."
Sleep 500ms
Enter

# Let the stream run
Sleep 1s

# Simulated provider starts pouring letters...
# Then the budget cutoff hits!
Sleep 8s

# Pause on the result
Sleep 4s
