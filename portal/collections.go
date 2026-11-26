package portal

import (
	"fmt"
	"log"
	"slices"
	"strings"
)

// portalCollection is a collection as the home page and the portal menu list
// it: its name over the links of its listed portals.
type portalCollection struct {
	Name  string
	Links []navLink
}

// collectPortals puts each portal into the collections whose match labels it
// holds, as matchCollections does, and writes each problem that
// matchCollections returns to the log.
func collectPortals(collections []Collection, sites []*site) error {
	problems, err := matchCollections(collections, sites)
	if err != nil {
		return err
	}
	for _, problem := range problems {
		log.Print(problem)
	}
	return nil
}

// matchCollections gives each site the names of the collections whose match
// labels its portal's labels all hold, and every site the name of every
// collection and a clone of each, so that a later write to collections
// reaches no request. It returns a problem for each portal of no collection and each
// collection of no portal, and refuses a collection without a name, without
// match labels, with a match label that is empty or blank, or with the name of another
// collection. Without collections it changes nothing.
func matchCollections(collections []Collection, sites []*site) (problems []string, err error) {
	if len(collections) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(collections))
	configured := make([]Collection, 0, len(collections))
	for i, g := range collections {
		switch {
		case strings.TrimSpace(g.Name) == "":
			return nil, fmt.Errorf("collection %d has no name", i+1)
		case slices.Contains(names, g.Name):
			return nil, fmt.Errorf("two collections have the name %q", g.Name)
		case len(g.MatchLabels) == 0:
			return nil, fmt.Errorf("collection %q has no match labels", g.Name)
		case slices.ContainsFunc(g.MatchLabels, func(l string) bool { return strings.TrimSpace(l) == "" }):
			return nil, fmt.Errorf("collection %q has an empty match label", g.Name)
		}
		names = append(names, g.Name)
		configured = append(configured, g.Clone())
	}
	matched := map[string]bool{}
	for _, s := range sites {
		s.collectionNames, s.collections, s.configured = names, nil, configured
		for _, g := range collections {
			if holdsAll(s.config.Labels, g.MatchLabels) {
				s.collections = append(s.collections, g.Name)
				matched[g.Name] = true
			}
		}
		if len(s.collections) == 0 {
			problems = append(problems, fmt.Sprintf("portal %q is in no collection and is not listed", s.name))
		}
	}
	for _, name := range names {
		if !matched[name] {
			problems = append(problems, fmt.Sprintf("collection %q matches no portal", name))
		}
	}
	return problems, nil
}

// holdsAll reports whether labels holds every label of match.
func holdsAll(labels, match []string) bool {
	for _, m := range match {
		if !slices.Contains(labels, m) {
			return false
		}
	}
	return true
}

// listPortals returns the listed portals among sites, the sites of the
// portals visible to a reader: every site without collections, and with
// collections each site of a collection, once, with the collections that hold
// a listed portal, each with the links of its portals, in the collections'
// order.
func listPortals(sites []*site) ([]*site, []portalCollection) {
	if len(sites) == 0 || sites[0].collectionNames == nil {
		return sites, nil
	}
	var listed []*site
	for _, s := range sites {
		if len(s.collections) > 0 {
			listed = append(listed, s)
		}
	}
	var collections []portalCollection
	for _, name := range sites[0].collectionNames {
		var members []*site
		for _, s := range listed {
			if slices.Contains(s.collections, name) {
				members = append(members, s)
			}
		}
		if len(members) > 0 {
			collections = append(collections, portalCollection{Name: name, Links: linksTo(members)})
		}
	}
	return listed, collections
}

// linksTo returns a link to the first section of each site under its
// portal's name.
func linksTo(sites []*site) []navLink {
	links := make([]navLink, 0, len(sites))
	for _, s := range sites {
		links = append(links, navLink{Label: s.name, URL: s.url()})
	}
	return links
}
