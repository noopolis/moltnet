package skills

import _ "embed"

//go:embed moltnet/SKILL.md
var moltnetSkill string

func MoltnetSkill() string {
	return moltnetSkill
}
