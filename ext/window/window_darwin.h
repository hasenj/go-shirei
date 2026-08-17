#ifndef WINDOW_DARWIN_H
#define WINDOW_DARWIN_H

void window_setNSWindowMinSize(void *nswindow_ptr, double minW, double minH);
void window_centerNSWindow(void *nswindow_ptr);
void window_positionNSWindow(void *nswindow_ptr, int x, int y);

#endif
