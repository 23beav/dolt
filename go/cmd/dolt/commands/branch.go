// Copyright 2019 Liquidata, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"

	"github.com/liquidata-inc/dolt/go/cmd/dolt/cli"
	"github.com/liquidata-inc/dolt/go/cmd/dolt/errhand"
	eventsapi "github.com/liquidata-inc/dolt/go/gen/proto/dolt/services/eventsapi/v1alpha1"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/doltdb"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/env"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/env/actions"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/ref"
	"github.com/liquidata-inc/dolt/go/libraries/utils/argparser"
	"github.com/liquidata-inc/dolt/go/libraries/utils/set"
)

var branchForceFlagDesc = "Reset {{.LessThan}}branchname{{.GreaterThan}} to {{.LessThan}}startpoint{{.GreaterThan}}, even if {{.LessThan}}branchname{{.GreaterThan}} exists already. Without {{.EmphasisLeft}}-f{{.EmphasisRight}}, {{.EmphasisLeft}}dolt branch{{.EmphasisRight}} refuses to change an existing branch. In combination with {{.EmphasisLeft}}-d{{.EmphasisRight}} (or {{.EmphasisLeft}}--delete{{.EmphasisRight}}), allow deleting the branch irrespective of its merged status. In combination with -m (or {{.EmphasisLeft}}--move{{.EmphasisRight}}), allow renaming the branch even if the new branch name already exists, the same applies for {{.EmphasisLeft}}-c{{.EmphasisRight}} (or {{.EmphasisLeft}}--copy{{.EmphasisRight}})."

var branchDocs = cli.CommandDocumentationContent{
	ShortDesc: `List, create, or delete branches`,
	LongDesc: `If {{.EmphasisLeft}}--list{{.EmphasisRight}} is given, or if there are no non-option arguments, existing branches are listed; the current branch will be highlighted with an asterisk.

The command's second form creates a new branch head named {{.LessThan}}branchname{{.GreaterThan}} which points to the current {{.EmphasisLeft}}HEAD{{.EmphasisRight}}, or {{.LessThan}}start-point{{.GreaterThan}} if given.

Note that this will create the new branch, but it will not switch the working tree to it; use {{.EmphasisLeft}}dolt checkout <newbranch>{{.EmphasisRight}} to switch to the new branch.

With a {{.EmphasisLeft}}-m{{.EmphasisRight}}, {{.LessThan}}oldbranch{{.GreaterThan}} will be renamed to {{.LessThan}}newbranch{{.GreaterThan}}. If {{.LessThan}}newbranch{{.GreaterThan}} exists, -f must be used to force the rename to happen.

The {{.EmphasisLeft}}-c{{.EmphasisRight}} options have the exact same semantics as {{.EmphasisLeft}}-m{{.EmphasisRight}}, except instead of the branch being renamed it will be copied to a new name.

With a {{.EmphasisLeft}}-d{{.EmphasisRight}}, {{.LessThan}}branchname{{.GreaterThan}} will be deleted. You may specify more than one branch for deletion.`,
	Synopsis: []string{
		`[--list] [-v] [-a]`,
		`[-f] {{.LessThan}}branchname{{.GreaterThan}} [{{.LessThan}}start-point{{.GreaterThan}}]`,
		`-m [-f] [{{.LessThan}}oldbranch{{.GreaterThan}}] {{.LessThan}}newbranch{{.GreaterThan}}`,
		`-c [-f] [{{.LessThan}}oldbranch{{.GreaterThan}}] {{.LessThan}}newbranch{{.GreaterThan}}`,
		`-d [-f] {{.LessThan}}branchname{{.GreaterThan}}...`,
	},
}

const (
	listFlag        = "list"
	forceFlag       = "force"
	copyFlag        = "copy"
	moveFlag        = "move"
	deleteFlag      = "delete"
	deleteForceFlag = "D"
	verboseFlag     = "verbose"
	allFlag         = "all"
)

type BranchCmd struct{}

// Name is returns the name of the Dolt cli command. This is what is used on the command line to invoke the command
func (cmd BranchCmd) Name() string {
	return "branch"
}

// Description returns a description of the command
func (cmd BranchCmd) Description() string {
	return "Create, list, edit, delete branches."
}

// Returns command
func (cmd BranchCmd) GetCommandDocumentation(commandStr string) cli.CommandDocumentation {
	ap := cmd.createArgParser()
	return cli.BuildCommandDocumentation(commandStr, branchDocs, ap)
}

func (cmd BranchCmd) createArgParser() *argparser.ArgParser {
	ap := argparser.NewArgParser()
	ap.ArgListHelp = append(ap.ArgListHelp, [2]string{"start-point", "A commit that a new branch should point at."})
	ap.SupportsFlag(listFlag, "", "List branches")
	ap.SupportsFlag(forceFlag, "f", branchForceFlagDesc)
	ap.SupportsFlag(copyFlag, "c", "Create a copy of a branch.")
	ap.SupportsFlag(moveFlag, "m", "Move/rename a branch")
	ap.SupportsFlag(deleteFlag, "d", "Delete a branch. The branch must be fully merged in its upstream branch.")
	ap.SupportsFlag(deleteForceFlag, "", "Shortcut for {{.EmphasisLeft}}--delete --force{{.EmphasisRight}}.")
	ap.SupportsFlag(verboseFlag, "v", "When in list mode, show the hash and commit subject line for each head")
	ap.SupportsFlag(allFlag, "a", "When in list mode, shows remote tracked branches")
	return ap
}

// EventType returns the type of the event to log
func (cmd BranchCmd) EventType() eventsapi.ClientEventType {
	return eventsapi.ClientEventType_BRANCH
}

// Exec executes the command
func (cmd BranchCmd) Exec(ctx context.Context, commandStr string, args []string, dEnv *env.DoltEnv) int {
	ap := cmd.createArgParser()
	help, usage := cli.HelpAndUsagePrinters(cli.BuildCommandDocumentation(commandStr, branchDocs, ap))
	apr := cli.ParseArgs(ap, args, help)

	switch {
	case apr.Contains(moveFlag):
		return moveBranch(ctx, dEnv, apr, usage)
	case apr.Contains(copyFlag):
		return copyBranch(ctx, dEnv, apr, usage)
	case apr.Contains(deleteFlag):
		return deleteBranches(ctx, dEnv, apr, usage)
	case apr.Contains(deleteForceFlag):
		return deleteForceBranches(ctx, dEnv, apr, usage)
	case apr.Contains(listFlag):
		return printBranches(ctx, dEnv, apr, usage)
	case apr.NArg() > 0:
		return createBranch(ctx, dEnv, apr, usage)
	default:
		return printBranches(ctx, dEnv, apr, usage)
	}
}

func printBranches(ctx context.Context, dEnv *env.DoltEnv, apr *argparser.ArgParseResults, _ cli.UsagePrinter) int {
	branchSet := set.NewStrSet(apr.Args())

	verbose := apr.Contains(verboseFlag)
	printAll := apr.Contains(allParam)

	branches, err := dEnv.DoltDB.GetRefs(ctx)

	if err != nil {
		return HandleVErrAndExitCode(errhand.BuildDError("error: failed to read refs from db").AddCause(err).Build(), nil)
	}

	currentBranch := dEnv.RepoState.CWBHeadRef()
	sort.Slice(branches, func(i, j int) bool {
		return branches[i].String() < branches[j].String()
	})

	for _, branch := range branches {
		if branchSet.Size() > 0 && !branchSet.Contains(branch.GetPath()) {
			continue
		}

		cs, _ := doltdb.NewCommitSpec("HEAD", branch.String())

		if branch.GetType() != ref.BranchRefType && !printAll {
			continue
		}

		commitStr := ""
		branchName := "  " + branch.GetPath()
		branchLen := len(branchName)
		if ref.Equals(branch, currentBranch) {
			branchName = "* " + color.GreenString(branch.GetPath())
		} else if branch.GetType() == ref.RemoteRefType {
			branchName = "  " + color.RedString("remotes/"+branch.GetPath())
			branchLen += len("remotes/")

		}

		if verbose {
			cm, err := dEnv.DoltDB.Resolve(ctx, cs)

			if err == nil {
				h, err := cm.HashOf()

				if err != nil {
					return HandleVErrAndExitCode(errhand.BuildDError("error: failed to hash commit").AddCause(err).Build(), nil)
				}

				commitStr = h.String()
			}
		}

		fmtStr := fmt.Sprintf("%%s%%%ds\t%%s", 48-branchLen)
		line := fmt.Sprintf(fmtStr, branchName, "", commitStr)

		cli.Println(line)
	}

	return 0
}

func moveBranch(ctx context.Context, dEnv *env.DoltEnv, apr *argparser.ArgParseResults, usage cli.UsagePrinter) int {
	if apr.NArg() != 2 {
		usage()
		return 1
	}

	force := apr.Contains(forceFlag)
	src := apr.Arg(0)
	dest := apr.Arg(1)
	err := actions.MoveBranch(ctx, dEnv, src, apr.Arg(1), force)

	var verr errhand.VerboseError
	if err != nil {
		if err == doltdb.ErrBranchNotFound {
			verr = errhand.BuildDError("fatal: branch '%s' not found", src).Build()
		} else if err == actions.ErrAlreadyExists {
			verr = errhand.BuildDError("fatal: A branch named '%s' already exists.", dest).Build()
		} else if err == doltdb.ErrInvBranchName {
			verr = errhand.BuildDError("fatal: '%s' is not a valid branch name.", dest).Build()
		} else if err == actions.ErrCOBranchDelete {
			verr = errhand.BuildDError("error: Cannot delete checked out branch '%s'", src).Build()
		} else {
			bdr := errhand.BuildDError("fatal: Unexpected error moving branch from '%s' to '%s'", src, dest)
			verr = bdr.AddCause(err).Build()
		}
	}

	return HandleVErrAndExitCode(verr, usage)
}

func copyBranch(ctx context.Context, dEnv *env.DoltEnv, apr *argparser.ArgParseResults, usage cli.UsagePrinter) int {
	if apr.NArg() != 2 {
		usage()
		return 1
	}

	force := apr.Contains(forceFlag)
	src := apr.Arg(0)
	dest := apr.Arg(1)
	err := actions.CopyBranch(ctx, dEnv, src, dest, force)

	var verr errhand.VerboseError
	if err != nil {
		if err == doltdb.ErrBranchNotFound {
			verr = errhand.BuildDError("fatal: branch '%s' not found", src).Build()
		} else if err == actions.ErrAlreadyExists {
			verr = errhand.BuildDError("fatal: A branch named '%s' already exists.", dest).Build()
		} else if err == doltdb.ErrInvBranchName {
			verr = errhand.BuildDError("fatal: '%s' is not a valid branch name.", dest).Build()
		} else {
			bdr := errhand.BuildDError("fatal: Unexpected error copying branch from '%s' to '%s'", src, dest)
			verr = bdr.AddCause(err).Build()
		}
	}

	return HandleVErrAndExitCode(verr, usage)
}

func deleteBranches(ctx context.Context, dEnv *env.DoltEnv, apr *argparser.ArgParseResults, usage cli.UsagePrinter) int {
	return handleDeleteBranches(ctx, dEnv, apr, usage, apr.Contains(forceFlag))
}

func deleteForceBranches(ctx context.Context, dEnv *env.DoltEnv, apr *argparser.ArgParseResults, usage cli.UsagePrinter) int {
	return handleDeleteBranches(ctx, dEnv, apr, usage, true)
}

func handleDeleteBranches(ctx context.Context, dEnv *env.DoltEnv, apr *argparser.ArgParseResults, usage cli.UsagePrinter, force bool) int {
	if apr.NArg() != 1 {
		usage()
		return 1
	}

	brName := apr.Arg(0)

	err := actions.DeleteBranch(ctx, dEnv, brName, force)

	var verr errhand.VerboseError
	if err != nil {
		if err == doltdb.ErrBranchNotFound {
			verr = errhand.BuildDError("fatal: branch '%s' not found", brName).Build()
		} else if err == actions.ErrCOBranchDelete {
			verr = errhand.BuildDError("error: Cannot delete checked out branch '%s'", brName).Build()
		} else {
			bdr := errhand.BuildDError("fatal: Unexpected error deleting '%s'", brName)
			verr = bdr.AddCause(err).Build()
		}
	}

	return HandleVErrAndExitCode(verr, usage)
}

func createBranch(ctx context.Context, dEnv *env.DoltEnv, apr *argparser.ArgParseResults, usage cli.UsagePrinter) int {
	if apr.NArg() == 0 || apr.NArg() > 2 {
		usage()
		return 1
	}

	newBranch := apr.Arg(0)
	startPt := "head"

	if apr.NArg() == 2 {
		startPt = apr.Arg(1)
	}

	verr := createBranchWithStartPt(ctx, dEnv, newBranch, startPt, apr.Contains(forceFlag))
	return HandleVErrAndExitCode(verr, usage)
}

func createBranchWithStartPt(ctx context.Context, dEnv *env.DoltEnv, newBranch, startPt string, force bool) errhand.VerboseError {
	err := actions.CreateBranch(ctx, dEnv, newBranch, startPt, force)

	if err != nil {
		if err == actions.ErrAlreadyExists {
			return errhand.BuildDError("fatal: A branch named '%s' already exists.", newBranch).Build()
		} else if err == doltdb.ErrInvBranchName {
			bdr := errhand.BuildDError("fatal: '%s' is an invalid branch name.", newBranch)
			return bdr.Build()
		} else if err == doltdb.ErrInvHash || doltdb.IsNotACommit(err) {
			bdr := errhand.BuildDError("fatal: '%s' is not a commit and a branch '%s' cannot be created from it", startPt, newBranch)
			return bdr.Build()
		} else {
			bdr := errhand.BuildDError("fatal: Unexpected error creating branch '%s'", newBranch)
			bdr.AddCause(err)
			return bdr.Build()
		}
	}

	return nil
}

func HandleVErrAndExitCode(verr errhand.VerboseError, usage cli.UsagePrinter) int {
	if verr != nil {
		if msg := verr.Verbose(); strings.TrimSpace(msg) != "" {
			cli.PrintErrln(msg)
		}

		if verr.ShouldPrintUsage() {
			usage()
		}

		return 1
	}

	return 0
}
