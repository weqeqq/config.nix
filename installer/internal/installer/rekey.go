package installer

import "fmt"

func RunRekey(repoRoot, ageKeyFile string) error {
	repoRoot, err := normalizeRepoRoot(repoRoot)
	if err != nil {
		return err
	}
	if err := ensureFlakeRepo(repoRoot); err != nil {
		return err
	}
	if err := RekeySharedSecrets(repoRoot, ageKeyFile); err != nil {
		return err
	}
	fmt.Printf("Updated .sops.yaml and rekeyed shared secrets in %s\n", repoRoot)
	return nil
}
