package gitflow

import "errors"

var ErrBranchTypeUnknown = errors.New("branch type could not be determined")
var ErrAmbiguousBranchType = errors.New("branch matches multiple branch type prefixes")
