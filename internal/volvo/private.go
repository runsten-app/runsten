package volvo

// PrivateFields are the paths of the fields of the Volvo Cars API's responses that are
// not the vehicle's, which the readings' CSV file leaves out: operationId names the
// request, as the location's response gives it, and differs on each. None is secret.
func PrivateFields() []string { return []string{"operationId"} }
