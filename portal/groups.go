package portal

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
	// HOLE(2): match the groups, and log each problem
	return nil
}

// matchGroups gives each site of sites the names of the groups of groups
// whose match labels its portal's labels all hold, and every site the name
// of every group. It returns a problem for each portal of no group and each
// group of no portal, and refuses a group without a name, without match
// labels, or with the name of another group. Without groups it changes
// nothing.
func matchGroups(groups []Group, sites []*site) (problems []string, err error) {
	// HOLE(2): check the groups and match each portal's labels
	return nil, nil
}

// listPortals returns the listed portals among sites, the sites of the
// portals visible to a reader: every site without groups, and with groups
// each site of a group, once, with the groups that hold a listed portal,
// each with the links of its portals, in the groups' order.
func listPortals(sites []*site) ([]*site, []portalGroup) {
	// HOLE(3): with groups, keep the sites of a group and list them by group
	return sites, nil
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
