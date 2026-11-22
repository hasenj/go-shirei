package menu

import (
	"errors"
	"reflect"
	"testing"
)

func TestUpdateRejectsDuplicateIDs(t *testing.T) {
	model := Model{ApplicationName: "Test", Menus: []Menu{{Label: "File", Items: []Item{{ID: "open", Label: "Open"}, {ID: "open", Label: "Again"}}}}}
	if _, err := Update(model); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("Update error=%v, want ErrInvalidModel", err)
	}
}

func TestUpdateAcceptsNestedAndSeparatorItems(t *testing.T) {
	model := Model{ApplicationName: "Test", Menus: []Menu{{Label: "File", Items: []Item{
		{Kind: SeparatorItem},
		{Kind: SubmenuItem, Label: "Recent", Children: []Item{{ID: "one", Label: "One", Enabled: true}}},
	}}}}
	if _, err := Update(model); err != nil {
		t.Fatal(err)
	}
}

func TestCloneModelDoesNotAliasChildren(t *testing.T) {
	model := Model{ApplicationName: "Test", Menus: []Menu{{Label: "File", Items: []Item{{Kind: SubmenuItem, Label: "Recent", Children: []Item{{ID: "one", Label: "One"}}}}}}}
	clone := cloneModel(model)
	clone.Menus[0].Items[0].Children[0].Label = "Changed"
	if model.Menus[0].Items[0].Children[0].Label != "One" {
		t.Fatal("clone aliases nested menu children")
	}
}

func TestCloneModelPreservesEquality(t *testing.T) {
	model := Model{ApplicationName: "Test", Menus: []Menu{{Label: "File", Items: []Item{{Kind: CommandItem, ID: "open", Label: "Open", Enabled: true}}}}}
	clone := cloneModel(model)
	if !reflect.DeepEqual(model, clone) {
		t.Fatalf("clone changed model semantics: %#v != %#v", model, clone)
	}
}
