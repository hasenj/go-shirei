// landing-snippets renders the small code/output examples used on the Shirei
// landing page.
//
//	go run ./demos/landing-snippets
//	go run ./demos/landing-snippets --png form out.png
//	go run ./demos/landing-snippets --png tags out.png
//	go run ./demos/landing-snippets --png conditional out.png
//
//go:generate go run . --png form ../../../static-sites/judi.systems/shirei/snippets/form.png
//go:generate go run . --png tags ../../../static-sites/judi.systems/shirei/snippets/tags.png
//go:generate go run . --png conditional ../../../static-sites/judi.systems/shirei/snippets/conditional.png
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type Profile struct {
	Name  string
	Email string
	Saved bool
}

type Article struct {
	Title   string
	Summary string
	Tags    []string
}

type Contact struct {
	Name      string
	Email     string
	ShowEmail bool
}

var sampleProfile = Profile{
	Name:  "Mina Okafor",
	Email: "mina@example.com",
	Saved: true,
}

var sampleArticle = Article{
	Title:   "Building a fast cross-platform diff viewer",
	Summary: "A native Go tool with background parsing and a virtualized file list.",
	Tags:    []string{"Go", "Desktop", "Tooling", "Performance", "Open source"},
}

var sampleContact = Contact{
	Name:      "Lin Chen",
	Email:     "lin@example.com",
	ShowEmail: true,
}

func main() {
	if len(os.Args) >= 4 && os.Args[1] == "--png" {
		if err := renderScene(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	app.SetupWindow("Shirei landing snippets", 600, 760)
	app.Run(RootView)
}

func renderScene(name, out string) error {
	var width, height int
	var view func()

	switch name {
	case "form":
		width, height = 500, 250
		view = func() { ProfileForm(&sampleProfile) }
	case "tags":
		width, height = 500, 170
		view = func() { ArticleCard(&sampleArticle) }
	case "conditional":
		width, height = 500, 180
		view = func() { ContactCard(&sampleContact) }
	default:
		return fmt.Errorf("unknown scene %q: use form, tags, or conditional", name)
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return RenderToPNG(out, width, height, scene(view))
}

func scene(view func()) func() {
	return func() {
		Container(Attrs(Viewport, Pad(24), Background(220, 18, 96, 1)), func() {
			snippetCard(view)
		})
	}
}

func RootView() {
	Container(Attrs(Viewport, Pad(24), Gap(18), Background(220, 18, 96, 1)), func() {
		snippetCard(func() { ProfileForm(&sampleProfile) })
		snippetCard(func() { ArticleCard(&sampleArticle) })
		snippetCard(func() { ContactCard(&sampleContact) })
	})
}

func snippetCard(view func()) {
	Container(Attrs(Expand, Corners(10), Background(0, 0, 100, 1), BoxShadow(2)), view)
}

func ProfileForm(profile *Profile) {
	Container(Attrs(Expand, Pad(18), Gap(8)), func() {
		Label("Profile", FontSize(22), FontWeight(WeightBold))

		Label("Name")
		TextInput(&profile.Name)

		Label("Email")
		TextInput(&profile.Email)

		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			if Button(SymITick, "Save") {
				profile.Saved = true
			}
			if profile.Saved {
				Label("All changes saved", FontSize(12), TextColor(145, 55, 34, 1))
			}
		})
	})
}

func ArticleCard(article *Article) {
	Container(Attrs(Expand, Pad(18), Gap(10)), func() {
		Label(article.Title, FontSize(20), FontWeight(WeightBold))
		Label(article.Summary)
		Container(Attrs(Row, Wrap, Gap(7)), func() {
			for _, tag := range article.Tags {
				Tag(tag)
			}
		})
	})
}

func Tag(label string) {
	Container(Attrs(Pad2(5, 10), Corners(14), Background(220, 32, 90, 1)), func() {
		Label(label, FontSize(12))
	})
}

func ContactCard(contact *Contact) {
	Container(Attrs(Expand, Pad(18), Gap(10)), func() {
		Label(contact.Name, FontSize(20), FontWeight(WeightBold))

		if Button(NoIcon, "Show email") {
			contact.ShowEmail = !contact.ShowEmail
		}

		if contact.ShowEmail {
			Container(Attrs(Expand, Pad(10), Corners(6), Background(145, 25, 92, 1)), func() {
				Label(contact.Email)
			})
		}
	})
}
