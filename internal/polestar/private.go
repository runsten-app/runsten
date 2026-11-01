package polestar

// PrivateFields are the paths of the fields of the Data Portal's responses that the
// readings' CSV file leaves out: none, every domain tells the vehicle only (its VIN, its
// readings and the portal's event of each).
func PrivateFields() []string { return []string{} }
