package imagebuild

import "testing"

func TestRequestValidateAcceptsProjectImageRepository(t *testing.T) {
	request := Request{
		ProjectID:       "proj-1",
		ReleaseID:       "rel-1",
		RepositoryURL:   "https://github.com/example/repository",
		CommitSHA:       "0123456789abcdef0123456789abcdef01234567",
		ImageRepository: "registry.example.com/team/app",
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("validate with image_repository: %v", err)
	}
}

func TestRequestValidateRejectsInvalidImageRepository(t *testing.T) {
	base := Request{
		ProjectID:     "proj-1",
		ReleaseID:     "rel-1",
		RepositoryURL: "https://github.com/example/repository",
		CommitSHA:     "0123456789abcdef0123456789abcdef01234567",
	}
	cases := map[string]string{
		"tag":        "registry.example.com/team/app:v1",
		"digest":     "registry.example.com/team/app@sha256:abc",
		"no host":    "team/app",
		"bare host":  "registry.example.com",
		"uppercase":  "registry.example.com/Team/App",
		"whitespace": "registry.example.com/team app",
		"scheme":     "https://registry.example.com/team/app",
	}
	for name, repository := range cases {
		t.Run(name, func(t *testing.T) {
			request := base
			request.ImageRepository = repository
			if err := request.Validate(); err == nil {
				t.Fatalf("validate accepted invalid image_repository %q", repository)
			}
		})
	}
}

func TestRequestValidateRegistryCredentialMustMatchRepository(t *testing.T) {
	base := Request{ProjectID: "proj-1", ReleaseID: "rel-1", RepositoryURL: "https://github.com/example/repository", CommitSHA: "0123456789abcdef0123456789abcdef01234567", ImageRepository: "registry.example.com/team/app"}
	valid := base
	valid.RegistryCredential = PushCredential{ConnectionID: "acr-main", Registry: "registry.example.com", AuthType: "basic", Username: "robot", Secret: "password"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid registry credential rejected: %v", err)
	}
	cases := []Request{
		func() Request {
			value := base
			value.RegistryCredential = PushCredential{Registry: "other.example.com", AuthType: "basic", Username: "robot", Secret: "password"}
			return value
		}(),
		func() Request {
			value := base
			value.RegistryCredential = PushCredential{Registry: "registry.example.com", AuthType: "basic", Secret: "password"}
			return value
		}(),
		func() Request {
			value := base
			value.RegistryCredential = PushCredential{Registry: "registry.example.com", AuthType: "token", Secret: ""}
			return value
		}(),
	}
	for index, value := range cases {
		if err := value.Validate(); err == nil {
			t.Fatalf("case %d accepted invalid registry credential", index)
		}
	}
}

func TestRequestValidateRegistryCredentialCanDeriveRepository(t *testing.T) {
	request := Request{
		ProjectID:     "proj-1",
		ReleaseID:     "rel-1",
		RepositoryURL: "https://github.com/example/repository",
		CommitSHA:     "0123456789abcdef0123456789abcdef01234567",
		RegistryCredential: PushCredential{
			ConnectionID: "acr-main",
			Registry:     "registry.example.com",
			AuthType:     "basic",
			Username:     "robot",
			Secret:       "password",
		},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("validate with derived image_repository: %v", err)
	}
}
