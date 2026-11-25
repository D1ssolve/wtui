package ai

const DraftSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "description", "services"],
  "properties": {
    "title": {"type": "string"},
    "description": {"type": "string"},
    "services": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name", "bump", "reason", "tag_description"],
        "properties": {
          "name": {"type": "string"},
          "bump": {"type": "string", "enum": ["patch", "minor", "major"]},
          "reason": {"type": "string"},
          "tag_description": {"type": "string"}
        }
      }
    }
  }
}`

const ExplanationSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["cause", "steps"],
  "properties": {
    "cause": {"type": "string"},
    "steps": {"type": "array", "items": {"type": "string"}}
  }
}`

const WorkflowSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["yaml"],
  "properties": {
    "yaml": {"type": "string"}
  }
}`
