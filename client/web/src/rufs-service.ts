export class RufsShare {
  constructor(
    public local: string,
    public remote: string,
  )
  {}
}

export class RufsCircle {
  constructor(
    public name: string,
    public shares: RufsShare[],
  )
  {}
}

export class RufsConfig {
  constructor(
    public circles: RufsCircle[],
  )
  {}
}

export class RufsService {
  public static async getConfig(): Promise<RufsConfig> {
    const rq = await fetch('/api/config');
    if (!rq.ok) {
      throw new Error('Failed retrieving config: ' + rq.statusText);
    }
    const body = await rq.json();
    const circles: RufsCircle[] = [];
    body.Circles.forEach((circle: any) => {
      const shares: RufsShare[] = [];
      circle.Shares.forEach((share: any) => {
        shares.push(new RufsShare(share.Local, share.Remote));
      });
      circles.push(new RufsCircle(circle.Name, shares));
    });
    return new RufsConfig(circles);
  }
}