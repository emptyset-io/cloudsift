package config

// RegionToLocation maps AWS region codes to their location names for pricing API
var RegionToLocation = map[string]string{
	// US Regions
	"us-gov-east-1":      "AWS GovCloud (US-East)",
	"us-gov-west-1":      "AWS GovCloud (US-West)",
	"us-east-1":          "US East (N. Virginia)",
	"us-east-2":          "US East (Ohio)",
	"us-east-3":          "US East (Atlanta)",
	"us-east-4":          "US East (Boston)",
	"us-east-5":          "US East (Chicago)",
	"us-east-6":          "US East (Dallas)",
	"us-east-7":          "US East (Houston)",
	"us-east-8":          "US East (Kansas City 2)",
	"us-east-9":          "US East (Miami)",
	"us-east-10":         "US East (Minneapolis)",
	"us-east-11":         "US East (New York City)",
	"us-east-12":         "US East (Philadelphia)",
	"us-east-verizon-1":  "US East (Verizon) - Atlanta",
	"us-east-verizon-2":  "US East (Verizon) - Boston",
	"us-east-verizon-3":  "US East (Verizon) - Charlotte",
	"us-east-verizon-4":  "US East (Verizon) - Chicago",
	"us-east-verizon-5":  "US East (Verizon) - Dallas",
	"us-east-verizon-6":  "US East (Verizon) - Detroit",
	"us-east-verizon-7":  "US East (Verizon) - Houston",
	"us-east-verizon-8":  "US East (Verizon) - Miami",
	"us-east-verizon-9":  "US East (Verizon) - Minneapolis",
	"us-east-verizon-10": "US East (Verizon) - Nashville",
	"us-east-verizon-11": "US East (Verizon) - New York",
	"us-east-verizon-12": "US East (Verizon) - Tampa",
	"us-east-verizon-13": "US East (Verizon) - Washington DC",
	"us-west-1":          "US West (N. California)",
	"us-west-2":          "US West (Oregon)",
	"us-west-3":          "US West (Denver)",
	"us-west-4":          "US West (Honolulu)",
	"us-west-5":          "US West (Las Vegas)",
	"us-west-6":          "US West (Los Angeles)",
	"us-west-7":          "US West (Phoenix)",
	"us-west-8":          "US West (Portland)",
	"us-west-9":          "US West (Seattle)",
	"us-west-verizon-1":  "US West (Verizon) - Denver",
	"us-west-verizon-2":  "US West (Verizon) - Las Vegas",
	"us-west-verizon-3":  "US West (Verizon) - Los Angeles",
	"us-west-verizon-4":  "US West (Verizon) - Phoenix",
	"us-west-verizon-5":  "US West (Verizon) - San Francisco Bay Area",
	"us-west-verizon-6":  "US West (Verizon) - Seattle",

	// Canada
	"ca-central-1": "Canada (Central)",
	"ca-west-1":    "Canada West (Calgary)",
	"ca-toronto-1": "Canada (BELL) - Toronto",

	// South America
	"sa-east-1":    "South America (Sao Paulo)",
	"sa-west-1":    "Chile (Santiago)",
	"sa-south-1":   "Argentina (Buenos Aires)",
	"sa-central-1": "Peru (Lima)",

	// Europe
	"eu-central-1":  "EU (Frankfurt)",
	"eu-central-2":  "Europe (Zurich)",
	"eu-west-1":     "EU (Ireland)",
	"eu-west-2":     "EU (London)",
	"eu-west-3":     "EU (Paris)",
	"eu-south-1":    "EU (Milan)",
	"eu-south-2":    "Europe (Spain)",
	"eu-north-1":    "EU (Stockholm)",
	"eu-bt-1":       "Europe (British Telecom) - Manchester",
	"eu-vodafone-1": "Europe (Vodafone) - Berlin",
	"eu-vodafone-2": "Europe (Vodafone) - Dortmund",
	"eu-vodafone-3": "Europe (Vodafone) - London",
	"eu-vodafone-4": "Europe (Vodafone) - Manchester",
	"eu-vodafone-5": "Europe (Vodafone) - Munich",
	"eu-germany-1":  "Germany (Hamburg)",
	"eu-denmark-1":  "Denmark (Copenhagen)",
	"eu-finland-1":  "Finland (Helsinki)",

	// Africa
	"af-south-1": "Africa (Cape Town)",
	"af-north-1": "Morocco (Casablanca)",
	"af-west-1":  "Nigeria (Lagos)",

	// Middle East
	"me-south-1":   "Middle East (Bahrain)",
	"me-central-1": "Middle East (UAE)",
	"me-east-1":    "Israel (Tel Aviv)",
	"me-west-1":    "Oman (Muscat)",

	// Asia Pacific
	"ap-east-1":        "Asia Pacific (Hong Kong)",
	"ap-south-1":       "Asia Pacific (Mumbai)",
	"ap-south-2":       "Asia Pacific (Hyderabad)",
	"ap-southeast-1":   "Asia Pacific (Singapore)",
	"ap-southeast-2":   "Asia Pacific (Sydney)",
	"ap-southeast-3":   "Asia Pacific (Jakarta)",
	"ap-southeast-4":   "Asia Pacific (Melbourne)",
	"ap-southeast-5":   "Asia Pacific (Malaysia)",
	"ap-southeast-6":   "Asia Pacific (Thailand)",
	"ap-northeast-1":   "Asia Pacific (Tokyo)",
	"ap-northeast-2":   "Asia Pacific (Seoul)",
	"ap-northeast-3":   "Asia Pacific (Osaka)",
	"ap-northeast-4":   "Asia Pacific (KDDI) - Osaka",
	"ap-northeast-5":   "Asia Pacific (KDDI) - Tokyo",
	"ap-northeast-6":   "Asia Pacific (SKT) - Daejeon",
	"ap-northeast-7":   "Asia Pacific (SKT) - Seoul",
	"ap-india-1":       "India (Delhi)",
	"ap-india-2":       "India (Kolkata)",
	"ap-thailand-1":    "Thailand (Bangkok)",
	"ap-philippines-1": "Philippines (Manila)",
	"ap-taiwan-1":      "Taiwan (Taipei)",

	// Australia & New Zealand
	"au-southeast-1": "Australia (Perth)",
	"au-southeast-2": "New Zealand (Auckland)",
}

// GetLocationForRegion returns the location name for a given AWS region
func GetLocationForRegion(region string) (string, bool) {
	location, ok := RegionToLocation[region]
	return location, ok
}
