package schemas

// Event represents a single point or range on the timeline.
#Event: {
	slug?:           #Slug
	title:           string
	summary?:        string
	article?:        string
	date:            #Date
	end_date?:       #Date
	date_precision?: *"day" | "month" | "year"
	sort_order?:     int
	category_id?:    int
	tags?: [...string]
	sources?: [...#SourceInput]
	people?:  [...#EventPersonInput]
}

// Person involved in timeline events.
#Person: {
	slug?:      #Slug
	name:       string
	sort_name?: string
	bio?:       string
	born_date?: #Date
	died_date?: #Date
	photo_url?: string
}

// Category is a visual lane grouping for events.
#Category: {
	slug?:       #Slug
	name:        string
	color?:      string
	sort_order?: int
}

// SourceInput is a reference to an external article or document.
#SourceInput: {
	url:         string
	title?:      string
	author?:     string
	citation?:   string
	sort_order?: int
}

// EventPersonInput links a person to an event with context.
#EventPersonInput: {
	slug:       #Slug
	role?:      string
	explainer?: string
}

// EventLink connects two events.
#EventLink: {
	event_a_id:    int
	event_b_id:    int
	relationship?: string
	description?:  string
}

// EventPersonLink connects an event to a person.
#EventPersonLink: {
	event_id:    int
	person_id:   int
	role?:       string
	explainer?:  string
	sort_order?: int
}

// TagInput for adding a tag to an event.
#TagInput: {
	slug?: #Slug
	name?: string
}

// TimelineImport is the full bulk import format.
#TimelineImport: {
	name:         string
	slug?:        #Slug
	description?: string
	is_public?:   bool
	categories?:  [...#Category]
	tags?:        [...string]
	events:       [...#Event]
	people?:      [...#Person]
	event_links?: [...{
		event_a:       #Slug
		event_b:       #Slug
		relationship?: string
		description?:  string
	}]
}

// Primitives
#Slug: =~"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"
#Date: =~"^\\d{4}-\\d{2}-\\d{2}$"
