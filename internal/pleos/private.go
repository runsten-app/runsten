package pleos

// PrivateFields are the paths of the fields of the API's responses that the readings'
// CSV file leaves out: none, every response tells the vehicle alone (its readings, the
// state it sent them in, and the model its owner named). The client ID and the secret
// never ride in a response.
func PrivateFields() []string { return []string{} }
