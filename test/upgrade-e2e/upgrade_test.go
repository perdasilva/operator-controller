package upgradee2e

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/spf13/pflag"

	"github.com/operator-framework/operator-controller/test/e2e/steps"
)

// skipBoxcutterUpgradeScenarios skips scenarios tagged @skip-boxcutter-upgrade when the
// upgrade target is the experimental build. The Helm->ClusterObjectSet storage pivot is
// temporarily disabled, so an experimental upgrade (which runs the boxcutter reconciler)
// cannot migrate a pre-existing Helm-stored ClusterExtension. The standard upgrade path
// does not use the pivot and still runs these scenarios. The upgrade target is identified
// by RELEASE_UPGRADE, which points at the experimental install script for upgrades to the
// experimental build.
func skipBoxcutterUpgradeScenarios(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	const skipTag = "@skip-boxcutter-upgrade"
	if !strings.Contains(os.Getenv("RELEASE_UPGRADE"), "experimental") {
		return ctx, nil
	}
	for _, tag := range sc.Tags {
		if tag.Name == skipTag {
			return ctx, godog.ErrSkip
		}
	}
	return ctx, nil
}

var opts = godog.Options{
	Format:      "pretty",
	Paths:       []string{"features"},
	Output:      colors.Colored(os.Stdout),
	Concurrency: 1,
	NoColors:    true,
	Strict:      true,
}

func init() {
	godog.BindCommandLineFlags("godog.", &opts)
}

func TestMain(m *testing.M) {
	pflag.Parse()
	opts.Paths = pflag.Args()

	sc := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			sc.Before(skipBoxcutterUpgradeScenarios)
			sc.Before(steps.CreateScenarioContext)
			steps.RegisterSteps(sc)
		},
		Options: &opts,
	}.Run()

	if sc != 0 {
		log.Fatalf("non-zero status returned (%d), failed to run upgrade feature tests", sc)
	}
}
