Pod::Spec.new do |s|
  s.name           = 'PokitDeviceKey'
  s.version        = '1.0.0'
  s.summary        = 'Pokit cross-platform device identity (Android Keystore + iOS Secure Enclave)'
  s.description    = 'Pokit cross-platform device identity'
  s.license        = 'Proprietary'
  s.author         = { 'Pokit' => '' }
  s.homepage       = ''
  s.platforms      = { :ios => '16.4' }
  s.source         = { :git => '' }
  # Production sources only (top level). The XCTest under Tests/ imports XCTest
  # and must never land in the main library target — it is built by the
  # test_spec below (run via scripts/ios-native-gate.sh, M-track).
  s.source_files   = '*.{h,m,swift}'
  s.dependency       'ExpoModulesCore'

  s.test_spec 'Tests' do |test_spec|
    test_spec.source_files = 'Tests/**/*.swift'
  end
end
