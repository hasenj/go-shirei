package example

// Publish makes a completed file's matches visible to the UI.
func Publish(fileMatches []*Match) {
    WithFrameLock(func() {
        search.matches = append(search.matches, fileMatches...)
        search.filesMatched.Add(1)
    })
    RequestNextFrame()
}
