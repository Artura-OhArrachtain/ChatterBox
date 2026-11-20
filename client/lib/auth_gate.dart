import 'package:firebase_auth/firebase_auth.dart';
import 'package:flutter/material.dart';

import 'firebase_config.dart';

typedef AuthenticatedAppBuilder = Widget Function(User user);

class AuthGate extends StatefulWidget {
  const AuthGate({
    super.key,
    required this.authenticatedAppBuilder,
    this.configurationError,
  });

  final AuthenticatedAppBuilder authenticatedAppBuilder;
  final String? configurationError;

  @override
  State<AuthGate> createState() => _AuthGateState();
}

Future<Map<String, String>> apiAuthHeaders({bool jsonBody = false}) async {
  final user = FirebaseAuth.instance.currentUser;
  if (user == null || !user.emailVerified) {
    throw StateError('A verified signed-in account is required.');
  }
  final token = await user.getIdToken();
  if (token == null || token.isEmpty) {
    throw StateError('Could not refresh the account token.');
  }
  return {
    'Authorization': 'Bearer $token',
    if (jsonBody) 'Content-Type': 'application/json',
  };
}

class _AuthGateState extends State<AuthGate> {
  int _authRefresh = 0;

  void _refreshAuthState() => setState(() => _authRefresh++);

  @override
  Widget build(BuildContext context) {
    final error = widget.configurationError;
    if (error != null) {
      return _AuthMessageScreen(
        title: 'Firebase setup required',
        message: '$error\n\n${firebaseConfigurationHelp()}',
      );
    }

    return StreamBuilder<User?>(
      key: ValueKey(_authRefresh),
      stream: FirebaseAuth.instance.userChanges(),
      builder: (context, snapshot) {
        if (snapshot.hasError) {
          return const _AuthMessageScreen(
            title: 'Authentication unavailable',
            message: 'Could not check your account. Please try again later.',
          );
        }
        if (snapshot.connectionState == ConnectionState.waiting) {
          return const Scaffold(
            body: Center(child: CircularProgressIndicator()),
          );
        }

        final user = snapshot.data;
        if (user == null) return const SignInScreen();
        if (!user.emailVerified) {
          return EmailVerificationScreen(
            user: user,
            onVerified: _refreshAuthState,
          );
        }
        return widget.authenticatedAppBuilder(user);
      },
    );
  }
}

class SignInScreen extends StatefulWidget {
  const SignInScreen({super.key});

  @override
  State<SignInScreen> createState() => _SignInScreenState();
}

class _SignInScreenState extends State<SignInScreen> {
  final _formKey = GlobalKey<FormState>();
  final _nameController = TextEditingController();
  final _emailController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _registering = false;
  bool _working = false;
  String? _error;

  @override
  void dispose() {
    _nameController.dispose();
    _emailController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate() || _working) return;
    setState(() {
      _working = true;
      _error = null;
    });
    try {
      final auth = FirebaseAuth.instance;
      if (_registering) {
        final credential = await auth.createUserWithEmailAndPassword(
          email: _emailController.text.trim(),
          password: _passwordController.text,
        );
        final user = credential.user;
        if (user == null) {
          throw FirebaseAuthException(
            code: 'user-not-created',
            message: 'Account creation did not return a user.',
          );
        }
        await user.updateDisplayName(_nameController.text.trim());
        await user.sendEmailVerification();
        await auth.currentUser?.reload();
      } else {
        await auth.signInWithEmailAndPassword(
          email: _emailController.text.trim(),
          password: _passwordController.text,
        );
      }
    } on FirebaseAuthException catch (error) {
      if (mounted) setState(() => _error = _authErrorMessage(error));
    } catch (error) {
      if (mounted) {
        setState(() => _error = 'Could not complete sign-in: $error');
      }
    } finally {
      if (mounted) setState(() => _working = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: Form(
                key: _formKey,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const Icon(Icons.chat_bubble, size: 52),
                    const SizedBox(height: 16),
                    Text(
                      _registering ? 'Create your account' : 'Welcome back',
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.headlineSmall,
                    ),
                    const SizedBox(height: 24),
                    if (_registering) ...[
                      TextFormField(
                        controller: _nameController,
                        maxLength: 40,
                        textCapitalization: TextCapitalization.words,
                        decoration:
                            const InputDecoration(labelText: 'Display name'),
                        validator: (value) {
                          final name = value?.trim() ?? '';
                          if (name.isEmpty || name.runes.length > 40) {
                            return 'Enter a display name up to 40 characters.';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 12),
                    ],
                    TextFormField(
                      controller: _emailController,
                      keyboardType: TextInputType.emailAddress,
                      autofillHints: const [AutofillHints.email],
                      decoration: const InputDecoration(labelText: 'Email'),
                      validator: (value) {
                        final email = value?.trim() ?? '';
                        if (!email.contains('@') || email.startsWith('@')) {
                          return 'Enter a valid email address.';
                        }
                        return null;
                      },
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _passwordController,
                      obscureText: true,
                      autofillHints: [
                        _registering
                            ? AutofillHints.newPassword
                            : AutofillHints.password
                      ],
                      decoration: const InputDecoration(labelText: 'Password'),
                      validator: (value) {
                        final password = value ?? '';
                        if (password.length < 8) {
                          return 'Use a password with at least 8 characters.';
                        }
                        return null;
                      },
                    ),
                    if (_error != null) ...[
                      const SizedBox(height: 12),
                      Text(
                        _error!,
                        style: TextStyle(
                            color: Theme.of(context).colorScheme.error),
                      ),
                    ],
                    const SizedBox(height: 20),
                    FilledButton(
                      onPressed: _working ? null : _submit,
                      child: _working
                          ? const SizedBox(
                              height: 18,
                              width: 18,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : Text(_registering ? 'Create account' : 'Sign in'),
                    ),
                    TextButton(
                      onPressed: _working
                          ? null
                          : () => setState(() {
                                _registering = !_registering;
                                _error = null;
                              }),
                      child: Text(_registering
                          ? 'Already have an account? Sign in'
                          : 'New to ChatterBox? Create an account'),
                    ),
                    if (_registering)
                      const Text(
                        'You must verify your email before using ChatterBox.',
                        textAlign: TextAlign.center,
                      ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );
}

class EmailVerificationScreen extends StatefulWidget {
  const EmailVerificationScreen({
    super.key,
    required this.user,
    required this.onVerified,
  });

  final User user;
  final VoidCallback onVerified;

  @override
  State<EmailVerificationScreen> createState() =>
      _EmailVerificationScreenState();
}

class _EmailVerificationScreenState extends State<EmailVerificationScreen> {
  bool _working = false;
  String? _message;
  String? _error;

  Future<void> _resend() async {
    setState(() {
      _working = true;
      _message = null;
      _error = null;
    });
    try {
      await widget.user.sendEmailVerification();
      if (mounted) setState(() => _message = 'Verification email sent.');
    } on FirebaseAuthException catch (error) {
      if (mounted) setState(() => _error = _authErrorMessage(error));
    } finally {
      if (mounted) setState(() => _working = false);
    }
  }

  Future<void> _checkVerification() async {
    setState(() {
      _working = true;
      _message = null;
      _error = null;
    });
    try {
      await widget.user.reload();
      if (!mounted) return;
      final currentUser = FirebaseAuth.instance.currentUser;
      if (currentUser == null) {
        widget.onVerified();
        return;
      }
      await currentUser.getIdToken(true);
      if (!mounted) return;
      if (currentUser.emailVerified) {
        widget.onVerified();
      } else if (mounted) {
        setState(() => _error = 'Email is not verified yet. Check your inbox.');
      }
    } on FirebaseAuthException catch (error) {
      if (mounted) setState(() => _error = _authErrorMessage(error));
    } finally {
      if (mounted) setState(() => _working = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 460),
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Icon(Icons.mark_email_read_outlined, size: 48),
                  const SizedBox(height: 16),
                  Text(
                    'Verify your email',
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.headlineSmall,
                  ),
                  const SizedBox(height: 12),
                  Text(
                    'We sent a verification link to ${widget.user.email ?? 'your email address'}. '
                    'Verify it before continuing.',
                    textAlign: TextAlign.center,
                  ),
                  if (_message != null) ...[
                    const SizedBox(height: 12),
                    Text(_message!, textAlign: TextAlign.center),
                  ],
                  if (_error != null) ...[
                    const SizedBox(height: 12),
                    Text(
                      _error!,
                      textAlign: TextAlign.center,
                      style:
                          TextStyle(color: Theme.of(context).colorScheme.error),
                    ),
                  ],
                  const SizedBox(height: 20),
                  FilledButton(
                    onPressed: _working ? null : _checkVerification,
                    child: const Text('I verified my email'),
                  ),
                  TextButton(
                    onPressed: _working ? null : _resend,
                    child: const Text('Resend verification email'),
                  ),
                  TextButton(
                    onPressed:
                        _working ? null : () => FirebaseAuth.instance.signOut(),
                    child: const Text('Sign out'),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
}

class _AuthMessageScreen extends StatelessWidget {
  const _AuthMessageScreen({required this.title, required this.message});

  final String title;
  final String message;

  @override
  Widget build(BuildContext context) => Scaffold(
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 520),
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.lock_outline, size: 48),
                  const SizedBox(height: 16),
                  Text(title, style: Theme.of(context).textTheme.headlineSmall),
                  const SizedBox(height: 12),
                  Text(message, textAlign: TextAlign.center),
                ],
              ),
            ),
          ),
        ),
      );
}

String _authErrorMessage(FirebaseAuthException error) {
  switch (error.code) {
    case 'email-already-in-use':
      return 'An account already uses this email. Try signing in.';
    case 'invalid-email':
      return 'Enter a valid email address.';
    case 'weak-password':
      return 'Choose a stronger password.';
    case 'user-not-found':
    case 'wrong-password':
    case 'invalid-credential':
      return 'Email or password is incorrect.';
    case 'too-many-requests':
      return 'Too many attempts. Wait a little and try again.';
    case 'network-request-failed':
      return 'Network unavailable. Check your connection and try again.';
    default:
      return error.message ?? 'Authentication failed. Please try again.';
  }
}
