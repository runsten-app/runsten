package tesla

// PrivateFields are the paths of the fields of the Owner API's responses that are secret
// or not the vehicle's, which the readings' CSV file leaves out: the tokens of the
// phone key and the back seat, the account's access, and Tesla's identifiers of the
// user and of the car in their account, which its API calls are made with. The VIN and
// the car's name stay: they are the vehicle's.
func PrivateFields() []string {
	return []string{
		"response.tokens",
		"response.backseat_token",
		"response.backseat_token_updated_at",
		"response.user_id",
		"response.vehicle_id",
		"response.id",
		"response.id_s",
		"response.access_type",
		"response.granular_access",
	}
}
