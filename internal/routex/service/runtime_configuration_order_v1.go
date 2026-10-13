package service

func configurationOrderedSourceV1(s runtimeConfigurationSourceV1, p *runtimeConfigurationPricingV1) bool {
	if !configurationOrderedV1(s.ApprovalApplications, func(v runtimeConfigurationRegistrationApprovalApplicationV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.Egresses, func(v runtimeConfigurationEgressV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.Limits, func(v runtimeConfigurationResourceLimitV1) string { return v.ScopeKind + "\x00" + v.ScopeID }) {
		return false
	}
	if !configurationOrderedV1(s.Users, func(v runtimeConfigurationUserV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.Keys, func(v runtimeConfigurationAPIKeyV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.Scopes, func(v runtimeConfigurationAPIKeyModelV1) string { return v.KeyID + "\x00" + v.ModelID }) {
		return false
	}
	if !configurationOrderedV1(s.Grants, func(v runtimeConfigurationUserModelGrantV1) string { return v.UserID + "\x00" + v.ModelID }) {
		return false
	}
	if !configurationOrderedV1(s.Models, func(v runtimeConfigurationModelV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.Names, func(v runtimeConfigurationModelNameV1) string { return v.Name }) {
		return false
	}
	if !configurationOrderedV1(s.Providers, func(v runtimeConfigurationProviderV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.Connections, func(v runtimeConfigurationProviderConnectionV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.Credentials, func(v runtimeConfigurationProviderCredentialV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.ProviderModels, func(v runtimeConfigurationProviderModelV1) string { return v.ID }) {
		return false
	}
	if !configurationOrderedV1(s.Access, func(v runtimeConfigurationCredentialModelAccessV1) string {
		return v.CredentialID + "\x00" + v.ProviderModelID
	}) {
		return false
	}
	if !configurationOrderedV1(s.Attestations, func(v runtimeConfigurationCredentialDeploymentAttestationV1) string {
		return v.CredentialID + "\x00" + v.ProviderModelID
	}) {
		return false
	}
	if !configurationOrderedV1(s.Bindings, func(v runtimeConfigurationModelProviderBindingV1) string { return v.ID }) {
		return false
	}
	if s.ProjectData != nil {
		d := s.ProjectData
		if !configurationOrderedV1(d.Projects, func(v runtimeConfigurationProjectV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.Managers, func(v runtimeConfigurationProjectManagerV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.Keys, func(v runtimeConfigurationProjectKeyV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.Scopes, func(v runtimeConfigurationProjectKeyModelV1) string { return v.KeyID + "\x00" + v.ModelID }) {
			return false
		}
		if !configurationOrderedV1(d.Grants, func(v runtimeConfigurationProjectModelGrantV1) string { return v.ProjectID + "\x00" + v.ModelID }) {
			return false
		}
	}
	if s.TeamSessionData != nil {
		d := s.TeamSessionData
		if !configurationOrderedV1(d.Sessions, func(v runtimeConfigurationSessionV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.Teams, func(v runtimeConfigurationTeamV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.Memberships, func(v runtimeConfigurationTeamMembershipV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.Grants, func(v runtimeConfigurationTeamModelGrantV1) string { return v.TeamID + "\x00" + v.ModelID }) {
			return false
		}
		if !configurationOrderedV1(d.OIDCBindings, func(v runtimeConfigurationOIDCBindingV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.OAuthBindings, func(v runtimeConfigurationOAuthBindingV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.LDAPBindings, func(v runtimeConfigurationLDAPBindingV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.SAMLBindings, func(v runtimeConfigurationSAMLBindingV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(d.NamedIdentityBindings, func(v runtimeConfigurationNamedIdentityBindingV1) string { return v.ID }) {
			return false
		}
	}
	if p != nil {
		if !configurationOrderedV1(p.FX, func(v runtimeConfigurationPricingExchangeRateV1) string { return v.Currency }) {
			return false
		}
		if !configurationOrderedV1(p.Prices, func(v runtimeConfigurationModelPriceV1) string { return v.ID }) {
			return false
		}
		if !configurationOrderedV1(p.Rates, func(v runtimeConfigurationPriceRateV1) string { return v.ID }) {
			return false
		}
	}
	for _, policy := range s.LimitPolicies {
		if !configurationOrderedV1(policy.IPRanges, func(v string) string { return v }) {
			return false
		}
	}
	return true
}

func canonicalizeRuntimeConfigurationV1(document *runtimeConfigurationV1) {
	s := &document.Source
	p := document.PricingObservation
	configurationSortV1(s.ApprovalApplications, func(v runtimeConfigurationRegistrationApprovalApplicationV1) string { return v.ID })
	configurationSortV1(s.Egresses, func(v runtimeConfigurationEgressV1) string { return v.ID })
	configurationSortV1(s.Limits, func(v runtimeConfigurationResourceLimitV1) string { return v.ScopeKind + "\x00" + v.ScopeID })
	configurationSortV1(s.Users, func(v runtimeConfigurationUserV1) string { return v.ID })
	configurationSortV1(s.Keys, func(v runtimeConfigurationAPIKeyV1) string { return v.ID })
	configurationSortV1(s.Scopes, func(v runtimeConfigurationAPIKeyModelV1) string { return v.KeyID + "\x00" + v.ModelID })
	configurationSortV1(s.Grants, func(v runtimeConfigurationUserModelGrantV1) string { return v.UserID + "\x00" + v.ModelID })
	configurationSortV1(s.Models, func(v runtimeConfigurationModelV1) string { return v.ID })
	configurationSortV1(s.Names, func(v runtimeConfigurationModelNameV1) string { return v.Name })
	configurationSortV1(s.Providers, func(v runtimeConfigurationProviderV1) string { return v.ID })
	configurationSortV1(s.Connections, func(v runtimeConfigurationProviderConnectionV1) string { return v.ID })
	configurationSortV1(s.Credentials, func(v runtimeConfigurationProviderCredentialV1) string { return v.ID })
	configurationSortV1(s.ProviderModels, func(v runtimeConfigurationProviderModelV1) string { return v.ID })
	configurationSortV1(s.Access, func(v runtimeConfigurationCredentialModelAccessV1) string {
		return v.CredentialID + "\x00" + v.ProviderModelID
	})
	configurationSortV1(s.Attestations, func(v runtimeConfigurationCredentialDeploymentAttestationV1) string {
		return v.CredentialID + "\x00" + v.ProviderModelID
	})
	configurationSortV1(s.Bindings, func(v runtimeConfigurationModelProviderBindingV1) string { return v.ID })
	if s.ProjectData != nil {
		d := s.ProjectData
		configurationSortV1(d.Projects, func(v runtimeConfigurationProjectV1) string { return v.ID })
		configurationSortV1(d.Managers, func(v runtimeConfigurationProjectManagerV1) string { return v.ID })
		configurationSortV1(d.Keys, func(v runtimeConfigurationProjectKeyV1) string { return v.ID })
		configurationSortV1(d.Scopes, func(v runtimeConfigurationProjectKeyModelV1) string { return v.KeyID + "\x00" + v.ModelID })
		configurationSortV1(d.Grants, func(v runtimeConfigurationProjectModelGrantV1) string { return v.ProjectID + "\x00" + v.ModelID })
	}
	if s.TeamSessionData != nil {
		d := s.TeamSessionData
		configurationSortV1(d.Sessions, func(v runtimeConfigurationSessionV1) string { return v.ID })
		configurationSortV1(d.Teams, func(v runtimeConfigurationTeamV1) string { return v.ID })
		configurationSortV1(d.Memberships, func(v runtimeConfigurationTeamMembershipV1) string { return v.ID })
		configurationSortV1(d.Grants, func(v runtimeConfigurationTeamModelGrantV1) string { return v.TeamID + "\x00" + v.ModelID })
		configurationSortV1(d.OIDCBindings, func(v runtimeConfigurationOIDCBindingV1) string { return v.ID })
		configurationSortV1(d.OAuthBindings, func(v runtimeConfigurationOAuthBindingV1) string { return v.ID })
		configurationSortV1(d.LDAPBindings, func(v runtimeConfigurationLDAPBindingV1) string { return v.ID })
		configurationSortV1(d.SAMLBindings, func(v runtimeConfigurationSAMLBindingV1) string { return v.ID })
		configurationSortV1(d.NamedIdentityBindings, func(v runtimeConfigurationNamedIdentityBindingV1) string { return v.ID })
	}
	if p != nil {
		configurationSortV1(p.FX, func(v runtimeConfigurationPricingExchangeRateV1) string { return v.Currency })
		configurationSortV1(p.Prices, func(v runtimeConfigurationModelPriceV1) string { return v.ID })
		configurationSortV1(p.Rates, func(v runtimeConfigurationPriceRateV1) string { return v.ID })
	}
	for key, policy := range s.LimitPolicies {
		configurationSortV1(policy.IPRanges, func(v string) string { return v })
		s.LimitPolicies[key] = policy
	}
}
