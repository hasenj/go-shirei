#ifndef SHIREI_EXT_MENU_DARWIN_H
#define SHIREI_EXT_MENU_DARWIN_H

#include <stdlib.h>

int shirei_ext_menu_is_main_thread(void);
void shirei_ext_menu_begin(void);
void shirei_ext_menu_add_application_menu(const char *label);
void *shirei_ext_menu_add_menu(const char *label);
void *shirei_ext_menu_add_submenu(void *parent, const char *label);
void shirei_ext_menu_add_separator(void *parent);
void shirei_ext_menu_add_item(void *parent, const char *identifier, const char *label,
                              const char *key, int modifiers, int enabled, int checked, int role);
void shirei_ext_menu_commit(void);

#endif
