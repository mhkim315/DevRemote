Pod::Spec.new do |s|
  s.name           = 'PokitDeviceKey'
  s.version        = '1.0.0'
  s.summary        = 'Pokit cross-platform device identity (fail-closed stub until M3-auth-1B)'
  s.description    = 'Pokit cross-platform device identity'
  s.license        = 'Proprietary'
  s.author         = { 'Pokit' => '' }
  s.homepage       = ''
  s.platforms      = { :ios => '16.4' }
  s.source         = { :git => '' }
  s.source_files   = '**/*.{h,m,swift}'
  s.dependency       'ExpoModulesCore'
end
