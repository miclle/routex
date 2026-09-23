export interface User {
  id: string
  email: string
  name: string
  role: 'admin' | 'member'
}

export interface Session {
  user: User
  csrf_token: string
}

export interface LoginInput {
  email: string
  password: string
}

export interface SetupInput extends LoginInput {
  name: string
}
