import React from 'react';
import { NavigationContainer, DarkTheme, LinkingOptions } from '@react-navigation/native';
import { createBottomTabNavigator } from '@react-navigation/bottom-tabs';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { Ionicons } from '@expo/vector-icons';

import DashboardScreen from '../screens/dashboard/DashboardScreen';
import FeedScreen from '../screens/FeedScreen';
import SnippetsScreen from '../screens/SnippetsScreen';
import GlobalFeedScreen from '../screens/GlobalFeedScreen';

const Tab = createBottomTabNavigator();
const Stack = createNativeStackNavigator();

// P1b: deep link config — routes push notifications to the correct session.
const linking: LinkingOptions<{}> = {
  prefixes: ['pokit://'],
  config: {
    screens: {
      DashboardStack: {
        screens: {
          DashboardMain: 'dashboard',
          Terminal: 'session/:session',
        },
      },
    },
  },
};

function DashboardStackScreen({ route }: any) {
  const token = route.params?.token;
  const authCtx = route.params?.authCtx;
  return (
    <Stack.Navigator screenOptions={{ headerShown: false }}>
      <Stack.Screen name="DashboardMain">
        {(props) => (
          <DashboardScreen
            {...props}
            token={token}
            authCtx={authCtx}
            onSelectAgent={(session) => props.navigation.navigate('Terminal', { session })}
            onSnippets={() => props.navigation.navigate('Snippets')}
          />
        )}
      </Stack.Screen>
      <Stack.Screen name="Terminal">
        {(props: any) => (
          <FeedScreen
            {...props}
            session={props.route.params.session}
            token={token}
            authCtx={authCtx}
            onBack={() => props.navigation.goBack()}
          />
        )}
      </Stack.Screen>
      <Stack.Screen name="Snippets">
        {(props: any) => (
          <SnippetsScreen 
            {...props} 
            onBack={() => props.navigation.goBack()} 
          />
        )}
      </Stack.Screen>
    </Stack.Navigator>
  );
}

export function RootTabs({ token, authCtx }: { token?: string; authCtx?: any }) {
  return (
    <NavigationContainer theme={DarkTheme} linking={linking}>
      <Tab.Navigator
        screenOptions={({ route }) => ({
          headerShown: false,
          tabBarStyle: {
            backgroundColor: '#0D2D45',
            borderTopColor: '#1E91B3',
            borderTopWidth: 1,
            paddingBottom: 5,
            height: 60,
          },
          tabBarActiveTintColor: '#45EBE9',
          tabBarInactiveTintColor: '#8b949e',
          tabBarIcon: ({ focused, color, size }) => {
            let iconName: any = 'grid';
            if (route.name === 'DashboardStack') {
              iconName = focused ? 'grid' : 'grid-outline';
            } else if (route.name === 'Activity') {
              iconName = focused ? 'list' : 'list-outline';
            }
            return <Ionicons name={iconName} size={size} color={color} />;
          },
        })}
      >
        <Tab.Screen 
          name="DashboardStack" 
          component={DashboardStackScreen}
          initialParams={{ token, authCtx }}
          options={{ title: 'Dashboard' }} 
        />
        <Tab.Screen 
          name="Activity" 
          options={{ title: 'Activity' }}
        >
          {() => <GlobalFeedScreen token={token} />}
        </Tab.Screen>
      </Tab.Navigator>
    </NavigationContainer>
  );
}
