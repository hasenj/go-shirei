package example

func ScrollTo(index int) {
    list.firstVisible = index
    list.ensureVisible()
    RequestNextFrame()
}

func ScrollToEnd() {
    list.offset = list.maxOffset
    list.clampOffset()
    RequestNextFrame()
}
