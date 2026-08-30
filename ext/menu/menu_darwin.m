#import <Cocoa/Cocoa.h>
#include "menu_darwin.h"

extern void shireiExtMenuOnAction(char *identifier);

static NSMenu *sMainMenu = nil;
static NSMutableArray *sTargets = nil;

@interface ShireiMenuTarget : NSObject
@property(nonatomic, copy) NSString *identifier;
@end

@implementation ShireiMenuTarget
- (void)activate:(id)sender {
    const char *value = self.identifier.UTF8String;
    if (value) {
        shireiExtMenuOnAction((char *)value);
    }
}
@end

static NSMenu *menu_from(void *value) {
    return (__bridge NSMenu *)value;
}

int shirei_ext_menu_is_main_thread(void) {
    return [NSThread isMainThread] ? 1 : 0;
}

void shirei_ext_menu_begin(void) {
    sMainMenu = [[NSMenu alloc] initWithTitle:@"MainMenu"];
    sTargets = [[NSMutableArray alloc] init];
}

void *shirei_ext_menu_add_menu(const char *label) {
    NSString *title = [NSString stringWithUTF8String:label ?: ""];
    NSMenu *menu = [[NSMenu alloc] initWithTitle:title];
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:@""];
    [item setSubmenu:menu];
    [sMainMenu addItem:item];
    return (__bridge void *)menu;
}

void *shirei_ext_menu_add_submenu(void *parent, const char *label) {
    NSMenu *menu = menu_from(parent);
    NSString *title = [NSString stringWithUTF8String:label ?: ""];
    NSMenu *child = [[NSMenu alloc] initWithTitle:title];
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:@""];
    [item setSubmenu:child];
    [menu addItem:item];
    return (__bridge void *)child;
}

void shirei_ext_menu_add_separator(void *parent) {
    [menu_from(parent) addItem:[NSMenuItem separatorItem]];
}

static NSEventModifierFlags modifier_flags(int modifiers) {
    NSEventModifierFlags flags = 0;
    if (modifiers & 1) flags |= NSEventModifierFlagCommand;
    if (modifiers & 2) flags |= NSEventModifierFlagShift;
    if (modifiers & 4) flags |= NSEventModifierFlagOption;
    if (modifiers & 8) flags |= NSEventModifierFlagControl;
    return flags;
}

static void apply_role(NSMenuItem *item, int role) {
    SEL selector = NULL;
    switch (role) {
        case 1: selector = @selector(orderFrontStandardAboutPanel:); break;
        case 2: selector = @selector(terminate:); break;
        case 3: selector = @selector(terminate:); break;
        case 4: selector = @selector(hide:); break;
        case 5: selector = @selector(hideOtherApplications:); break;
        case 6: selector = @selector(unhideAllApplications:); break;
        case 7: selector = @selector(terminate:); break;
        default: break;
    }
    if (selector) [item setAction:selector];
}

void shirei_ext_menu_add_item(void *parent, const char *identifier, const char *label,
                              const char *key, int modifiers, int enabled, int checked, int role) {
    NSString *title = [NSString stringWithUTF8String:label ?: ""];
    NSString *equivalent = [NSString stringWithUTF8String:key ?: ""];
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:equivalent];
    [item setKeyEquivalentModifierMask:modifier_flags(modifiers)];
    [item setEnabled:enabled != 0];
    [item setState:checked ? NSControlStateValueOn : NSControlStateValueOff];
    apply_role(item, role);
    if (role == 0) {
        ShireiMenuTarget *target = [[ShireiMenuTarget alloc] init];
        target.identifier = [NSString stringWithUTF8String:identifier ?: ""];
        [sTargets addObject:target];
        [item setTarget:target];
        [item setAction:@selector(activate:)];
    }
    [menu_from(parent) addItem:item];
}

void shirei_ext_menu_commit(void) {
    [NSApp setMainMenu:sMainMenu];
}
