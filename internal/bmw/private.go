package bmw

// PrivateFields are the paths of the fields of CarData's responses that the readings' CSV
// file leaves out: the picture's bytes (Image), no reading. Every other field is the
// vehicle's (the basic data, the values, the charging history with its places, as the
// other sources' positions).
func PrivateFields() []string { return []string{"image"} }
