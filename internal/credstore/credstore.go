package credstore

import "github.com/zalando/go-keyring"

const service = "talented-cli"

func Save(profile, token, storage string) (string, error) {
	if storage == "file" {
		return "file", nil
	}
	if err := keyring.Set(service, profile, token); err == nil {
		return "keychain", nil
	} else if storage == "keychain" {
		return "", err
	}
	return "file", nil
}

func Get(profile string) (string, error) {
	return keyring.Get(service, profile)
}

func Delete(profile string) error {
	return keyring.Delete(service, profile)
}
