package portal

import (
	"fmt"
	"log"
	"slices"
)

// portalGroup is a group as the home page and the portal menu list it: its
// name over the links of its listed portals.
type portalGroup struct {
	Name  string
	Links []navLink
}

// groupPortals puts each portal into the groups whose match labels it holds,
// as matchGroups does, and writes each problem that matchGroups returns to
// the log.
func groupPortals(groups []Group, sites []*site) error {
	problems, err := matchGroups(groups, sites)
	if err != nil {
		return err
	}
	for _, problem := range problems {
		log.Print(problem)
	}
	return nil
}

// matchGroups gives each site of sites the names of the groups of groups
// whose match labels its portal's labels all hold, and every site the name
// of every group. It returns a problem for each portal of no group and each
// group of no portal, and refuses a group without a name, without match
// labels, or with the name of another group. Without groups it changes
// nothing.
func matchGroups(groups []Group, sites []*site) (problems []string, err error) {
	if len(groups) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(groups))
	for i, g := range groups {
		switch {
		case g.Name == "":
			return nil, fmt.Errorf("group %d has no name", i+1)
		case slices.Contains(names, g.Name):
			return nil, fmt.Errorf("two groups have the name %q", g.Name)
		case len(g.MatchLabels) == 0:
			return nil, fmt.Errorf("group %q has no match labels", g.Name)
		case slices.Contains(g.MatchLabels, ""):
			return nil, fmt.Errorf("group %q has an empty match label", g.Name)
		}
		names = append(names, g.Name)
	}
	matched := map[string]bool{}
	for _, s := range sites {
		s.grouped, s.groups = names, nil
		for _, g := range groups {
			if holdsAll(s.config.Labels, g.MatchLabels) {
				s.groups = append(s.groups, g.Name)
				matched[g.Name] = true
			}
		}
		if len(s.groups) == 0 {
			problems = append(problems, fmt.Sprintf("portal %q is in no group and is not listed", s.name))
		}
	}
	for _, name := range names {
		if !matched[name] {
			problems = append(problems, fmt.Sprintf("group %q matches no portal", name))
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
// portals visible to a reader: every site without groups, and with groups
// each site of a group, once, with the groups that hold a listed portal,
// each with the links of its portals, in the groups' order.
func listPortals(sites []*site) ([]*site, []portalGroup) {
	if len(sites) == 0 || sites[0].grouped == nil {
		return sites, nil
	}
	var listed []*site
	for _, s := range sites {
		if len(s.groups) > 0 {
			listed = append(listed, s)
		}
	}
	var groups []portalGroup
	for _, name := range sites[0].grouped {
		var members []*site
		for _, s := range listed {
			if slices.Contains(s.groups, name) {
				members = append(members, s)
			}
		}
		if len(members) > 0 {
			groups = append(groups, portalGroup{Name: name, Links: linksTo(members)})
		}
	}
	return listed, groups
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
