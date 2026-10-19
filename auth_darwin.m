//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>
#include <string.h>

char *vault_authenticate(void) {
    @autoreleasepool {
        LAContext *context = [[LAContext alloc] init];
        context.localizedFallbackTitle = @"";
        context.touchIDAuthenticationAllowableReuseDuration = 0;
        NSError *error = nil;
        LAPolicy policy = LAPolicyDeviceOwnerAuthenticationWithBiometrics;
        if (![context canEvaluatePolicy:policy error:&error]) {
            return strdup((error.localizedDescription ?: @"Touch ID is unavailable").UTF8String);
        }
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        __block BOOL authenticated = NO;
        __block NSString *failure = @"Authentication failed";
        [context evaluatePolicy:policy
                localizedReason:@"验证指纹以访问 vault"
                          reply:^(BOOL success, NSError *replyError) {
            authenticated = success;
            failure = replyError.localizedDescription ?: @"Authentication failed";
            dispatch_semaphore_signal(done);
        }];
        if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 60 * NSEC_PER_SEC)) != 0) {
            [context invalidate];
            return strdup("Authentication timed out");
        }
        [context invalidate];
        return authenticated ? NULL : strdup(failure.UTF8String);
    }
}
