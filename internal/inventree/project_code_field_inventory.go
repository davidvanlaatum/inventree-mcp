package inventree

// ProjectCodeFieldClass records the approved handling for every field in the
// pinned API 530 ProjectCode serializer. This inventory is exhaustive so
// schema drift cannot silently widen exact project-code output.
type ProjectCodeFieldClass string

const (
	ProjectCodeFieldExposed  ProjectCodeFieldClass = "exposed"
	ProjectCodeFieldDeferred ProjectCodeFieldClass = "deferred"
)

// ProjectCodeFieldInventory classifies every pinned ProjectCode serializer
// field. Per the F-S48 operator decision, ProjectCode was excluded from the
// owner/responsible object matrix, so `responsible`/`responsible_detail`
// remain deferred pending a separately approved story.
var ProjectCodeFieldInventory = map[string]ProjectCodeFieldClass{
	"pk":                 ProjectCodeFieldExposed,
	"code":               ProjectCodeFieldExposed,
	"description":        ProjectCodeFieldExposed,
	"active":             ProjectCodeFieldExposed,
	"responsible":        ProjectCodeFieldDeferred,
	"responsible_detail": ProjectCodeFieldDeferred,
}
