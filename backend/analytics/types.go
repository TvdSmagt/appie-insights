package analytics

// --- Response types ---

type Receipt struct {
	TransactionID string   `json:"transaction_id"`
	Date          string   `json:"date"`
	TotalAmount   float64  `json:"total_amount"`
	ItemCount     int      `json:"item_count"`
	MatchedCount  int      `json:"matched_count"`
	CO2EqTotal    *float64 `json:"co2eq_total"`
	WeightTotal   *float64 `json:"weight_total"`
	DiscountTotal *float64 `json:"discount_total"`
}

type ReceiptItem struct {
	ID              int      `json:"id"`
	Description     string   `json:"description"`
	WebTitle        string   `json:"web_title"`
	Quantity        int      `json:"quantity"`
	Amount          float64  `json:"amount"`
	WebID           *int     `json:"web_id"`
	PosID           *int     `json:"pos_id"`
	AHCategory      string   `json:"ah_category"`
	CO2EqCategory   string   `json:"co2eq_category"`
	CO2EqName       string   `json:"co2eq_name"`
	CO2EqPerKg      *float64 `json:"co2eq_per_kg"`
	MatchMethod     string   `json:"match_method"`
	WeightKg        *float64 `json:"weight_kg"`
	UnitSize        string   `json:"unit_size"`
	ThumbnailURL    string   `json:"thumbnail_url"`
	WeightPerUnitKg *float64 `json:"weight_per_unit_kg"`
	CO2EqTotal      *float64 `json:"co2eq_total"`
}

type ReceiptDetail struct {
	Receipt
	CO2EqPerEuro *float64      `json:"co2eq_per_euro"`
	Items        []ReceiptItem `json:"items"`
}

type Item struct {
	SourceType      string   `json:"source_type"`
	Description     string   `json:"description"`
	Quantity        int      `json:"quantity"`
	Amount          float64  `json:"amount"`
	Date            string   `json:"date"`
	CO2EqCategory   string   `json:"co2eq_category"`
	CO2EqName       string   `json:"co2eq_name"`
	CO2EqPerKg      *float64 `json:"co2eq_per_kg"`
	MatchMethod     string   `json:"match_method"`
	WeightKg        *float64 `json:"weight_kg"`
	UnitSize        string   `json:"unit_size"`
	WebID           *int     `json:"web_id"`
	WebTitle        string   `json:"web_title"`
	WeightPerUnitKg *float64 `json:"weight_per_unit_kg"`
	CO2EqTotal      *float64 `json:"co2eq_total"`
}

type Product struct {
	WebID                int      `json:"web_id"`
	ThumbnailURL         string   `json:"thumbnail_url"`
	Title                string   `json:"title"`
	Brand                string   `json:"brand"`
	AHCategory           string   `json:"ah_category"`
	AHSubcategory        string   `json:"ah_subcategory"`
	UnitSize             string   `json:"unit_size"`
	Nutriscore           string   `json:"nutriscore"`
	UnitPriceDescription string   `json:"unit_price_description"`
	PropertyIcons        string   `json:"property_icons"`
	CO2EqPerKg           *float64 `json:"co2eq_per_kg"`
	CO2EqCategory        string   `json:"co2eq_category"`
	WeightPerUnitKg      *float64 `json:"weight_per_unit_kg"`
	CO2EqPerUnit         *float64 `json:"co2eq_per_unit"`
}

type ProductDetail struct {
	WebID                int      `json:"web_id"`
	ThumbnailURL         string   `json:"thumbnail_url"`
	Title                string   `json:"title"`
	Brand                string   `json:"brand"`
	AHCategory           string   `json:"ah_category"`
	AHSubcategory        string   `json:"ah_subcategory"`
	UnitSize             string   `json:"unit_size"`
	Nutriscore           string   `json:"nutriscore"`
	UnitPriceDescription string   `json:"unit_price_description"`
	PropertyIcons        string   `json:"property_icons"`
	CO2EqPerKg           *float64 `json:"co2eq_per_kg"`
	CO2EqCategory        string   `json:"co2eq_category"`
	PosID                *int     `json:"pos_id"`
	CO2EqName            string   `json:"co2eq_name"`
	MatchMethod          string   `json:"match_method"`
	NetContent           string   `json:"net_content"`
	ServingSize          string   `json:"serving_size"`
	WeightKg             *float64 `json:"weight_kg"`
	WeightPerUnitKg      *float64 `json:"weight_per_unit_kg"`
	// WeightSource names the source that set WeightKg (unit_size, net_content,
	// serving_size, multipack, default, correction), or "" if none/unenriched.
	WeightSource string `json:"weight_source"`
	// WeightBreakdown lists the per-source candidate weights, in priority order,
	// for display on the product page. Exactly one entry is marked Active.
	WeightBreakdown []WeightSourceValue `json:"weight_breakdown"`
}

// WeightSourceValue is one row of the product-page weight breakdown: a source,
// the kg it yields (nil when that source has no value), and whether it is the
// source actually used for this product.
type WeightSourceValue struct {
	Source  string   `json:"source"`
	ValueKg *float64 `json:"value_kg"`
	Active  bool     `json:"active"`
}

type ProductPurchase struct {
	Date        string   `json:"date"`
	Description string   `json:"description"`
	Quantity    int      `json:"quantity"`
	Amount      float64  `json:"amount"`
	UnitPrice   *float64 `json:"unit_price"`
	Source      string   `json:"source"`
}

type Order struct {
	OrderID        int      `json:"order_id"`
	DeliveryDate   string   `json:"delivery_date"`
	DeliveryMethod string   `json:"delivery_method"`
	DeliveryStatus string   `json:"delivery_status"`
	TotalPrice     float64  `json:"total_price"`
	ItemCount      int      `json:"item_count"`
	MatchedCount   int      `json:"matched_count"`
	CO2EqTotal     *float64 `json:"co2eq_total"`
	WeightTotal    *float64 `json:"weight_total"`
	DiscountTotal  *float64 `json:"discount_total"`
}

type OrderItem struct {
	WebID           *int     `json:"web_id"`
	Title           string   `json:"title"`
	Brand           string   `json:"brand"`
	Category        string   `json:"category"`
	SalesUnitSize   string   `json:"sales_unit_size"`
	Quantity        int      `json:"quantity"`
	AllocatedQty    int      `json:"allocated_qty"`
	UnitPrice       *float64 `json:"unit_price"`
	WasPrice        *float64 `json:"was_price"`
	ImageURL        string   `json:"image_url"`
	CO2EqCategory   string   `json:"co2eq_category"`
	CO2EqName       string   `json:"co2eq_name"`
	CO2EqPerKg      *float64 `json:"co2eq_per_kg"`
	WeightPerUnitKg *float64 `json:"weight_per_unit_kg"`
	CO2EqTotal      *float64 `json:"co2eq_total"`
	LineTotal       float64  `json:"line_total"`
}

type OrderDetail struct {
	OrderID         int         `json:"order_id"`
	DeliveryDate    string      `json:"delivery_date"`
	TotalPrice      float64     `json:"total_price"`
	DeliveryMethod  string      `json:"delivery_method"`
	InvoiceID       string      `json:"invoice_id"`
	AddressStreet   string      `json:"address_street"`
	AddressNumber   string      `json:"address_number"`
	AddressExtra    string      `json:"address_extra"`
	AddressPostcode string      `json:"address_postcode"`
	AddressCity     string      `json:"address_city"`
	CO2EqTotal      *float64    `json:"co2eq_total"`
	CO2EqPerEuro    *float64    `json:"co2eq_per_euro"`
	Items           []OrderItem `json:"items"`
}

type SearchProduct struct {
	WebID        int    `json:"web_id"`
	ThumbnailURL string `json:"thumbnail_url"`
	Title        string `json:"title"`
	Brand        string `json:"brand"`
	AHCategory   string `json:"ah_category"`
	UnitSize     string `json:"unit_size"`
}

type SearchReceiptItem struct {
	Date          string  `json:"date"`
	TransactionID string  `json:"transaction_id"`
	Description   string  `json:"description"`
	Quantity      int     `json:"quantity"`
	Amount        float64 `json:"amount"`
}

type SearchOrderItem struct {
	DeliveryDate string  `json:"delivery_date"`
	OrderID      int     `json:"order_id"`
	Title        string  `json:"title"`
	Brand        string  `json:"brand"`
	Quantity     int     `json:"quantity"`
	Amount       float64 `json:"amount"`
}

type SearchResults struct {
	Products     []SearchProduct     `json:"products"`
	ReceiptItems []SearchReceiptItem `json:"receipt_items"`
	OrderItems   []SearchOrderItem   `json:"order_items"`
}

type ProductStats struct {
	WebID        int      `json:"web_id"`
	Title        string   `json:"title"`
	ThumbnailURL string   `json:"thumbnail_url"`
	TimesBought  int      `json:"times_bought"`
	TotalSpent   float64  `json:"total_spent"`
	TotalKg      float64  `json:"total_kg"`
	CO2EqTotal   *float64 `json:"co2eq_total"`
}

type NutriscoreEntry struct {
	Score       string `json:"score"`
	Count       int    `json:"count"`
	TimesBought int    `json:"times_bought"`
}

type NutriscoreProductStats struct {
	WebID        int      `json:"web_id"`
	ThumbnailURL string   `json:"thumbnail_url"`
	Title        string   `json:"title"`
	Nutriscore   string   `json:"nutriscore"`
	TimesBought  int      `json:"times_bought"`
	TotalSpent   float64  `json:"total_spent"`
	TotalKg      float64  `json:"total_kg"`
	CO2EqTotal   *float64 `json:"co2eq_total"`
}

type PosProductInfo struct {
	PosID        int    `json:"pos_id"`
	Description  string `json:"description"`
	WebID        *int   `json:"web_id"`
	Title        string `json:"title"`
	ThumbnailURL string `json:"thumbnail_url"`
	InNotFound   bool   `json:"in_not_found"`
}

type MissingCategoryItem struct {
	WebID       int      `json:"web_id"`
	Title       string   `json:"title"`
	AHCategory  string   `json:"ah_category"`
	UnitSize    string   `json:"unit_size"`
	CO2EqName   string   `json:"co2eq_name"`
	CO2EqPerKg  *float64 `json:"co2eq_per_kg"`
	WeightKg    *float64 `json:"weight_kg"`
	MatchMethod string   `json:"match_method"`
}

type MissingWeightItem struct {
	WebID         int      `json:"web_id"`
	Title         string   `json:"title"`
	UnitSize      string   `json:"unit_size"`
	CO2EqName     string   `json:"co2eq_name"`
	CO2EqCategory string   `json:"co2eq_category"`
	CO2EqPerKg    *float64 `json:"co2eq_per_kg"`
	WeightKg      *float64 `json:"weight_kg"`
}

type ProductIssueSummary struct {
	TotalFoodProducts      int `json:"total_food_products"`
	NoWebID                int `json:"no_web_id"`
	NoPosID                int `json:"no_pos_id"`
	NoProductData          int `json:"no_product_data"`
	NoWeight               int `json:"no_weight"`
	UnmatchedSubcategories int `json:"unmatched_subcategories"`
	// UnmatchedNoSubcategory counts products in match_method='unmatched' that have
	// no AH subcategory string at all, so they cannot be fixed via the subcategory
	// map — they need product-data refetch or a per-product correction instead.
	UnmatchedNoSubcategory int `json:"unmatched_no_subcategory"`
}

// UnmatchedSubcategory is one AH subcategory, under a food category, that is not
// in the subcategory map (so its products land in match_method='unmatched' with
// no CO₂ factor). The subcategory — not the individual product — is the unit of
// decision: mapping it in ah_subcategory_map.csv resolves every product under it.
type UnmatchedSubcategory struct {
	AHCategory    string   `json:"ah_category"`
	AHSubcategory string   `json:"ah_subcategory"`
	ProductCount  int      `json:"product_count"`
	ExampleTitles []string `json:"example_titles"`
}

type NoWebIDItem struct {
	PosID       int    `json:"pos_id"`
	Description string `json:"description"`
}

type ProductIssueItem struct {
	WebID          int      `json:"web_id"`
	PosID          *int     `json:"pos_id"`
	Title          string   `json:"title"`
	AHCategory     string   `json:"ah_category"`
	AHSubcategory  string   `json:"ah_subcategory"`
	UnitSize       string   `json:"unit_size"`
	WeightKg       *float64 `json:"weight_kg"`
	CO2EqName      string   `json:"co2eq_name"`
	CO2EqCategory  string   `json:"co2eq_category"`
	CO2EqPerKg     *float64 `json:"co2eq_per_kg"`
	POSDescription string   `json:"pos_description"`
}

type ProductIssues struct {
	Summary                ProductIssueSummary    `json:"summary"`
	NoWebID                []NoWebIDItem          `json:"no_web_id"`
	NoPosID                []ProductIssueItem     `json:"no_pos_id"`
	NoProductData          []ProductIssueItem     `json:"no_product_data"`
	NoWeight               []ProductIssueItem     `json:"no_weight"`
	UnmatchedSubcategories []UnmatchedSubcategory `json:"unmatched_subcategories"`
	// UnmatchedNoSubcategory lists unmatched food products that carry no AH
	// subcategory string, so they cannot be fixed via the subcategory map — they
	// need product-data refetch or a per-product correction instead.
	UnmatchedNoSubcategory []ProductIssueItem `json:"unmatched_no_subcategory"`
}

type FinancialSummary struct {
	TotalSpent          float64 `json:"total_spent"`
	AvgPerYear          float64 `json:"avg_per_year"`
	AvgPerMonth         float64 `json:"avg_per_month"`
	AvgPerWeek          float64 `json:"avg_per_week"`
	TotalDiscount       float64 `json:"total_discount"`
	DiscountAvgPerYear  float64 `json:"discount_avg_per_year"`
	DiscountAvgPerMonth float64 `json:"discount_avg_per_month"`
	DiscountAvgPerWeek  float64 `json:"discount_avg_per_week"`
	FirstDate           string  `json:"first_date"`
	LastDate            string  `json:"last_date"`
}

type CategorySpending struct {
	Category    string  `json:"category"`
	Subcategory string  `json:"subcategory"`
	TotalSpent  float64 `json:"total_spent"`
}

type PeriodSpending struct {
	Period   string  `json:"period"`
	Amount   float64 `json:"amount"`
	Discount float64 `json:"discount"`
}

type DiscountStats struct {
	Name          string  `json:"name"`
	TotalDiscount float64 `json:"total_discount"`
}

type SustainabilitySummary struct {
	Grade               string   `json:"grade"`
	PctAboveSustainable *float64 `json:"pct_above_sustainable"`
	TopCategory         string   `json:"top_category"`
	AvgKgPerAePerMonth  *float64 `json:"avg_kg_per_ae_per_month"`
}

type TrendEntry struct {
	Period   string  `json:"period"`
	Category string  `json:"category"`
	CO2Eq    float64 `json:"co2eq"`
}

type CategoryCO2 struct {
	Category string  `json:"category"`
	CO2Eq    float64 `json:"co2eq"`
}

type CategoryProduct struct {
	Description     string   `json:"description"`
	WebTitle        string   `json:"web_title"`
	CO2EqName       string   `json:"co2eq_name"`
	Quantity        float64  `json:"quantity"`
	Amount          float64  `json:"amount"`
	CO2EqTotal      float64  `json:"co2eq_total"`
	CO2EqPerKg      *float64 `json:"co2eq_per_kg"`
	WeightPerUnitKg *float64 `json:"weight_per_unit_kg"`
	WebID           *int     `json:"web_id"`
	PctOfCategory   float64  `json:"percentage_of_category"`
}
